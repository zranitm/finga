// Package repository is the item service's Postgres store behind application.Store.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/dbx"
	"finga/pkg/idempotency"
	"finga/pkg/idempotency/idempotencypg"
	"finga/pkg/outbox/outboxpg"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/internal/repository/internal/db"
)

// Postgres is the service's store; every error it returns is classified into the model's.
type Postgres struct {
	pool *pgxpool.Pool
	obs  dbx.Observer // hears each transaction; nil hears none
}

// NewPostgres builds the store over a pool, telling obs about each transaction.
func NewPostgres(pool *pgxpool.Pool, obs dbx.Observer) *Postgres {
	return &Postgres{pool: pool, obs: obs}
}

// InTx runs fn in one transaction; fn may be retried, so it must have no outside side effects.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(p.unitOfWork(tx))
	}, dbx.Observed(p.obs))
	return failures.Classify(err)
}

func (p *Postgres) unitOfWork(tx pgx.Tx) application.UnitOfWork {
	q, now := db.New(tx), dbx.Now()
	return unitOfWork{
		items:   repo{q: q, now: now},
		events:  eventRepo{q: q, now: now},
		notices: notices{w: outboxpg.NewWriter(tx, now)},
		idem:    idem{store: idempotencypg.NewStore(tx, now)},
	}
}

// Inventory lists the owner's inventory, by tab and slot, read with its revision from one snapshot.
func (p *Postgres) Inventory(ctx context.Context, owner kernel.CharacterID) (model.Inventory, error) {
	var inv model.Inventory
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		revision, err := q.InventoryRevision(ctx, uuid.UUID(owner))
		if err != nil {
			return err
		}
		rows, err := q.ListInventory(ctx, uuid.UUID(owner))
		if err != nil {
			return err
		}
		items, err := withRolls(ctx, q, rows)
		if err != nil {
			return err
		}
		inv = model.Inventory{Revision: uint64(revision), Items: items} //nolint:gosec // a revision is never negative
		return nil
	}, dbx.Observed(p.obs), dbx.Snapshot())
	if err != nil {
		return model.Inventory{}, failures.Classify(err)
	}
	return inv, nil
}

// unitOfWork stamps every row it writes with now, the transaction's time on the app's clock.
type unitOfWork struct {
	items   repo
	events  eventRepo
	notices notices
	idem    idem
}

// Items is the item repository bound to the transaction.
func (u unitOfWork) Items() application.Repository { return u.items }

// Events is the event repository bound to the transaction.
func (u unitOfWork) Events() application.EventRepository { return u.events }

// Notices sends notices in the transaction.
func (u unitOfWork) Notices() application.Notices { return u.notices }

// Idempotency is the idempotency store bound to the transaction.
func (u unitOfWork) Idempotency() application.Idempotency { return u.idem }

type repo struct {
	q   *db.Queries
	now dbx.Stamp
}

// RecordStarterGrant inserts the owner's grant unless it has one.
func (r repo) RecordStarterGrant(ctx context.Context, owner kernel.CharacterID) (bool, error) {
	n, err := r.q.RecordStarterGrant(ctx, db.RecordStarterGrantParams{CharacterID: uuid.UUID(owner), Now: r.now})
	return n == 1, err
}

// LockInventory takes the owner's lock, then locks the owner's inventory rows in ID order.
func (r repo) LockInventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) {
	if err := r.q.LockOwner(ctx, owner.String()); err != nil {
		return nil, err
	}
	rows, err := r.q.LockInventory(ctx, uuid.UUID(owner))
	if err != nil {
		return nil, err
	}
	items, err := withRolls(ctx, r.q, rows)
	if err != nil {
		return nil, err
	}
	model.SortInventory(items)
	return items, nil
}

// LockOwned locks one item that belongs to owner; no row otherwise.
func (r repo) LockOwned(ctx context.Context, id kernel.ItemID, owner kernel.CharacterID) (model.Item, error) {
	row, err := r.q.LockOwnedItem(ctx, db.LockOwnedItemParams{ID: uuid.UUID(id), OwnerID: uuid.UUID(owner)})
	if err != nil {
		return model.Item{}, err
	}
	if row.Location != "inventory" {
		return model.Item{}, pgx.ErrNoRows
	}
	items, err := withRolls(ctx, r.q, []db.Item{row})
	if err != nil {
		return model.Item{}, err
	}
	return items[0], nil
}

// Insert adds an inventory row, and equipment's roll beside it.
func (r repo) Insert(ctx context.Context, it model.Item) error {
	tab, slot := string(it.Tab), int32(it.Slot) //nolint:gosec // slots are small
	err := r.q.InsertItem(ctx, db.InsertItemParams{
		ID: uuid.UUID(it.ID), TemplateID: it.TemplateID, OwnerID: uuid.UUID(it.Owner), Tab: &tab, Slot: &slot,
		Quantity: int32(it.Quantity), //nolint:gosec // quantities are small
		Now:      r.now,
	})
	if err != nil || it.Stats == nil {
		return err
	}
	s := it.Stats
	//nolint:gosec // stats are small
	return r.q.InsertRoll(ctx, db.InsertRollParams{
		ItemID: uuid.UUID(it.ID), Attack: int32(s.Attack), Defense: int32(s.Defense),
		Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
		Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion), CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage),
		MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP), Now: r.now,
	})
}

// SetQuantity changes a quantity.
func (r repo) SetQuantity(ctx context.Context, id kernel.ItemID, quantity int) error {
	n, err := r.q.SetQuantity(ctx, db.SetQuantityParams{ID: uuid.UUID(id), Quantity: int32(quantity), Now: r.now}) //nolint:gosec // quantities are small
	return dbx.RowsOrNotFound(n, err)
}

// SetSlot moves an item to a slot.
func (r repo) SetSlot(ctx context.Context, id kernel.ItemID, tab gamedata.Tab, slot int) error {
	t, s := string(tab), int32(slot) //nolint:gosec // slots are small
	n, err := r.q.SetSlot(ctx, db.SetSlotParams{ID: uuid.UUID(id), Tab: &t, Slot: &s, Now: r.now})
	return dbx.RowsOrNotFound(n, err)
}

// NextRevision counts one more change to the owner's inventory.
func (r repo) NextRevision(ctx context.Context, owner kernel.CharacterID) (uint64, error) {
	revision, err := r.q.NextRevision(ctx, db.NextRevisionParams{OwnerID: uuid.UUID(owner), Now: r.now})
	return uint64(revision), err //nolint:gosec // a revision is never negative
}

// Destroy marks an item destroyed with quantity 0 rather than deleting the row.
func (r repo) Destroy(ctx context.Context, id kernel.ItemID) error {
	n, err := r.q.DestroyItem(ctx, db.DestroyItemParams{ID: uuid.UUID(id), Now: r.now})
	return dbx.RowsOrNotFound(n, err)
}

type eventRepo struct {
	q   *db.Queries
	now dbx.Stamp
}

// Append adds one line to the item history.
func (r eventRepo) Append(ctx context.Context, e model.Event) error {
	params := db.InsertItemEventParams{
		ItemID: uuid.UUID(e.ItemID), Kind: string(e.Kind), QuantityDelta: int32(e.QuantityDelta), //nolint:gosec // quantities are small
		CauseType: string(e.Cause.Type), OccurredAt: e.OccurredAt, Now: r.now,
	}
	if e.FromOwner != nil {
		u := uuid.UUID(*e.FromOwner)
		params.FromOwner = &u
	}
	if e.ToOwner != nil {
		u := uuid.UUID(*e.ToOwner)
		params.ToOwner = &u
	}
	if e.Cause.ID != uuid.Nil {
		u := e.Cause.ID
		params.CauseID = &u
	}
	return r.q.InsertItemEvent(ctx, params)
}

// idem adapts pkg/idempotency, storing the Result as a reply.
type idem struct{ store idempotency.Store }

// Run runs fn once per key for the account.
func (i idem) Run(ctx context.Context, owner kernel.AccountID, scope, key string, fingerprint []byte, fn func() (application.Result, error)) (application.Result, error) {
	raw, err := i.store.Do(ctx, owner.String(), scope, key, fingerprint, func() ([]byte, error) {
		res, err := fn()
		if err != nil {
			return nil, err
		}
		return encodeReply(res)
	})
	if err != nil {
		if errors.Is(err, idempotency.ErrKeyReused) {
			return application.Result{}, fmt.Errorf("%w: %w", model.ErrKeyReused, err)
		}
		return application.Result{}, err
	}
	return decodeReply(raw)
}

// withRolls is the items read, each with its roll if it has one.
func withRolls(ctx context.Context, q *db.Queries, rows []db.Item) ([]model.Item, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	rolls, err := q.ListRolls(ctx, ids)
	if err != nil {
		return nil, err
	}
	byItem := make(map[uuid.UUID]*progression.ItemStats, len(rolls))
	for _, r := range rolls {
		byItem[r.ItemID] = &progression.ItemStats{
			Attack: int(r.Attack), Defense: int(r.Defense), STR: int(r.Str), DEX: int(r.Dex), INT: int(r.Int), AGI: int(r.Agi), VIT: int(r.Vit),
			Accuracy: int(r.Accuracy), Evasion: int(r.Evasion), CritRate: int(r.CritRate), CritDamage: int(r.CritDamage), MaxHP: int(r.MaxHp), MaxMP: int(r.MaxMp),
		}
	}
	out := make([]model.Item, 0, len(rows))
	for _, r := range rows {
		it := toItem(r)
		it.Stats = byItem[r.ID]
		out = append(out, it)
	}
	return out, nil
}

func toItem(r db.Item) model.Item {
	it := model.Item{ID: kernel.ItemID(r.ID), TemplateID: r.TemplateID, Owner: kernel.CharacterID(r.OwnerID), Quantity: int(r.Quantity)}
	if r.Tab != nil {
		it.Tab = gamedata.Tab(*r.Tab)
	}
	if r.Slot != nil {
		it.Slot = int(*r.Slot)
	}
	return it
}

const (
	constraintSlot     = "one_item_per_slot"
	constraintQuantity = "items_quantity_check"
)

// failures maps a missing row and constraint errors to the model's, as dbx makes timeouts and outages transient.ErrUnavailable; each keeps its cause, so InTx's retries still see 40001.
var failures = dbx.Errors{
	NotFound: model.ErrNotFound,
	Constraints: map[string]error{
		constraintSlot:     model.ErrSlotTaken,
		constraintQuantity: model.ErrBadQuantity,
	},
}
