// Package item is the item module's facade: inventories, and every change to them, each under an idempotency key; its types and kernel errors are the module's contract.
package item

import (
	"context"
	"time"

	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/modules/item/config"
	"finga/modules/item/internal/application"
	"finga/modules/item/internal/model"
	"finga/modules/item/internal/repository"
	"finga/pkg/idempotency/idempotencypg"
	"finga/pkg/outbox/outboxpg"
	"finga/pkg/outbox/outboxprom"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
)

// Module is the item module; every call to it goes through these methods.
type Module struct {
	calls *facade.Calls
	svc   service
}

// service is the module's application, as the facade calls it.
type service interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error)
	Add(ctx context.Context, actor model.Actor, in application.AddInput) (application.Result, error)
	AddMany(ctx context.Context, actor model.Actor, drops []application.AddInput) (application.Batch, error)
	Move(ctx context.Context, actor model.Actor, in application.MoveInput) (application.Result, error)
	Discard(ctx context.Context, actor model.Actor, in application.DiscardInput) (application.Result, error)
	DiscardMany(ctx context.Context, actor model.Actor, in application.DiscardManyInput) (application.Result, error)
	Use(ctx context.Context, actor model.Actor, in application.UseInput) (application.Result, bool, error)
	GrantStarter(ctx context.Context, actor model.Actor) (application.Result, error)
	Equip(ctx context.Context, actor model.Actor, in application.EquipInput) (application.Result, error)
	Unequip(ctx context.Context, actor model.Actor, in application.UnequipInput) (application.Result, error)
	HandIn(ctx context.Context, actor model.Actor, in application.HandInInput) (application.Result, error)
	HandedIn(ctx context.Context, owner kernel.CharacterID, asked []application.Completion) ([]application.HandIn, error)
}

// outboxCountEvery is how often the finga_outbox_unpublished gauge is counted again.
const outboxCountEvery = 15 * time.Second

// Deps are what the module is built from.
type Deps struct {
	Module *host.Module // its logs, panics, tasks, metrics and the process's database
	Config config.Config
	Data   *gamedata.Data
}

// New builds the module and adds its outbox gauge and retention tasks; it fails fast on an unreachable database.
func New(ctx context.Context, d Deps) (*Module, error) {
	m, cfg, log := d.Module, d.Config, d.Module.Logger
	outboxMetrics, err := outboxprom.New(m.Registry(), "item")
	if err != nil {
		return nil, err
	}
	retentionMetrics, err := retentionprom.New(m.Registry(), "item")
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := m.Database(ctx)
	if err != nil {
		return nil, err
	}
	m.Task("outbox gauge", func(ctx context.Context) error {
		return outboxMetrics.Track(ctx, outboxCountEvery, func(ctx context.Context) (int64, error) { return outboxpg.Unpublished(ctx, pool) }, log)
	}, host.Restartable)
	keys := retention.Policy{Keep: cfg.IdempotencyKeys.Keep, Every: cfg.IdempotencyKeys.PruneEvery, Batch: cfg.IdempotencyKeys.PruneBatch, Gap: cfg.IdempotencyKeys.PruneGap}
	pruneKeys := func(ctx context.Context, before time.Time, limit int) (int64, error) {
		return idempotencypg.Prune(ctx, pool, before, limit)
	}
	m.Task("idempotency key retention", func(ctx context.Context) error {
		return retention.Run(ctx, "idempotency_keys", keys, pruneKeys, retentionMetrics, log)
	}, host.Restartable)
	outboxRows := func(keep time.Duration) retention.Policy {
		return retention.Policy{Keep: keep, Every: cfg.Outbox.PruneEvery, Batch: cfg.Outbox.PruneBatch, Gap: cfg.Outbox.PruneGap}
	}
	m.Task("outbox published retention", func(ctx context.Context) error {
		prune := func(ctx context.Context, before time.Time, limit int) (int64, error) {
			return outboxpg.PrunePublished(ctx, pool, before, limit)
		}
		return retention.Run(ctx, "outbox", outboxRows(cfg.Outbox.PublishedKeep), prune, retentionMetrics, log.With("rows", "published"))
	}, host.Restartable)
	if cfg.Outbox.UnpublishedKeep > 0 { // Stage 1 only: with Stage 2's relay it would delete events not yet sent
		m.Task("outbox unpublished retention", func(ctx context.Context) error {
			prune := func(ctx context.Context, before time.Time, limit int) (int64, error) {
				return outboxpg.PruneUnpublished(ctx, pool, before, limit)
			}
			return retention.Run(ctx, "outbox", outboxRows(cfg.Outbox.UnpublishedKeep), prune, retentionMetrics, log.With("rows", "unpublished"))
		}, host.Restartable)
	}
	calls, err := m.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	svc := application.NewService(application.Deps{Store: repository.NewPostgres(pool, dbMetrics), Content: d.Data, Logger: log})
	return &Module{calls: calls, svc: svc}, nil
}

// Inventory lists a character's items, those in its tabs then those it wears.
func (m *Module) Inventory(ctx context.Context, owner kernel.CharacterID) (_ Inventory, err error) {
	ctx, call := m.calls.Start(ctx, "Inventory")
	defer call.End(&err)
	inv, err := m.svc.Inventory(ctx, owner)
	return Inventory{Items: itemsOf(inv.Items), Revision: inv.Revision}, err
}

// Add puts a quantity of a template in the inventory.
func (m *Module) Add(ctx context.Context, actor Actor, r AddRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "Add")
	defer call.End(&err)
	in, err := r.input(false) // cause, then key
	if err != nil {
		return Result{}, err
	}
	res, err := m.svc.Add(ctx, model.Actor(actor), in)
	return resultOf(res), err
}

// MaxBatch is the most drops one AddMany takes.
const MaxBatch = application.MaxBatch

// AddMany grants a batch of drops for one owner; a rule refuses a drop alone, and its outcome says which.
func (m *Module) AddMany(ctx context.Context, actor Actor, drops []AddRequest) (_ Batch, err error) {
	ctx, call := m.calls.Start(ctx, "AddMany")
	defer call.End(&err)
	if len(drops) == 0 || len(drops) > MaxBatch {
		return Batch{}, model.ErrBadBatch
	}
	out := Batch{Outcomes: make([]Outcome, len(drops))}
	var ins []application.AddInput
	var at []int // each input's drop
	for i, d := range drops {
		in, err := d.input(true) // key, then cause
		if err != nil {
			out.Outcomes[i].Err = errorTable.Kernel(err)
			continue
		}
		ins, at = append(ins, in), append(at, i)
	}
	if len(ins) == 0 {
		return out, nil
	}
	b, err := m.svc.AddMany(ctx, model.Actor(actor), ins)
	if err != nil {
		return Batch{}, err
	}
	for k, o := range b.Outcomes {
		out.Outcomes[at[k]] = Outcome{Err: errorTable.Kernel(o.Err), Replayed: o.Replayed, Result: resultOf(o.Result)}
	}
	out.Changed, out.Revision = slotsOf(b.Changed), b.Revision
	return out, nil
}

// Move moves an item within its tab.
func (m *Module) Move(ctx context.Context, actor Actor, r MoveRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "Move")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, err
	}
	res, err := m.svc.Move(ctx, model.Actor(actor), application.MoveInput(r))
	return resultOf(res), err
}

// Discard destroys an item or part of a stack.
func (m *Module) Discard(ctx context.Context, actor Actor, r DiscardRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "Discard")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, err
	}
	res, err := m.svc.Discard(ctx, model.Actor(actor), application.DiscardInput(r))
	return resultOf(res), err
}

// DiscardMany destroys whole items or stacks, all of them or none.
func (m *Module) DiscardMany(ctx context.Context, actor Actor, r DiscardManyRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "DiscardMany")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, err
	}
	res, err := m.svc.DiscardMany(ctx, model.Actor(actor), application.DiscardManyInput(r))
	return resultOf(res), err
}

// Use takes one of a usable item from its stack; replayed says the key was used before, and the result is that use's.
func (m *Module) Use(ctx context.Context, actor Actor, r UseRequest) (_ Result, replayed bool, err error) {
	ctx, call := m.calls.Start(ctx, "Use")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, false, err
	}
	res, replayed, err := m.svc.Use(ctx, model.Actor(actor), application.UseInput(r))
	return resultOf(res), replayed, err
}

// GrantStarter gives a new character its starter items, once.
func (m *Module) GrantStarter(ctx context.Context, actor Actor) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "GrantStarter")
	defer call.End(&err)
	res, err := m.svc.GrantStarter(ctx, model.Actor(actor))
	return resultOf(res), err
}

// Equip wears an inventory item, checked against the level, class and own stats the caller sends.
func (m *Module) Equip(ctx context.Context, actor Actor, r EquipRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "Equip")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, err
	}
	res, err := m.svc.Equip(ctx, model.Actor(actor), application.EquipInput(r))
	return resultOf(res), err
}

// Unequip puts a worn item back in the Equip tab.
func (m *Module) Unequip(ctx context.Context, actor Actor, r UnequipRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "Unequip")
	defer call.End(&err)
	if err := checkKey(r.Key); err != nil {
		return Result{}, err
	}
	res, err := m.svc.Unequip(ctx, model.Actor(actor), application.UnequipInput(r))
	return resultOf(res), err
}

// HandIn takes the items a quest asks for and gives its rewards, once per completion.
func (m *Module) HandIn(ctx context.Context, actor Actor, r HandInRequest) (_ Result, err error) {
	ctx, call := m.calls.Start(ctx, "HandIn")
	defer call.End(&err)
	res, err := m.svc.HandIn(ctx, model.Actor(actor), r.input())
	return resultOf(res), err
}

// HandedIn is which of the completions asked about the character handed in, each with the reply it got.
func (m *Module) HandedIn(ctx context.Context, owner kernel.CharacterID, asked []Completion) (_ []HandedIn, err error) {
	ctx, call := m.calls.Start(ctx, "HandedIn")
	defer call.End(&err)
	in := make([]application.Completion, 0, len(asked))
	for _, c := range asked {
		in = append(in, application.Completion(c))
	}
	found, err := m.svc.HandedIn(ctx, owner, in)
	out := make([]HandedIn, 0, len(found))
	for _, h := range found {
		out = append(out, HandedIn{Quest: h.Quest, Completion: h.Completion, Result: resultOf(h.Result)})
	}
	return out, err
}
