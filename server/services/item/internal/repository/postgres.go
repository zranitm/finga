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
	q    *db.Queries
	obs  dbx.Observer // hears each transaction; nil hears none
}

// NewPostgres builds the store over a pool, telling obs about each transaction.
func NewPostgres(pool *pgxpool.Pool, obs dbx.Observer) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool), obs: obs}
}

// InTx runs fn in one transaction; fn may be retried, so it must have no outside side effects.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(p.unitOfWork(tx))
	}, dbx.Observed(p.obs))
	return failures.Classify(err)
}

func (p *Postgres) unitOfWork(tx pgx.Tx) application.UnitOfWork {
	q := db.New(tx)
	return unitOfWork{
		items:   repo{q: q},
		events:  eventRepo{q: q},
		notices: notices{w: outboxpg.NewWriter(tx)},
		idem:    idem{store: idempotencypg.NewStore(tx)},
	}
}

// Inventory lists the owner's inventory, by tab and slot.
func (p *Postgres) Inventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) {
	rows, err := p.q.ListInventory(ctx, uuid.UUID(owner))
	if err != nil {
		return nil, failures.Classify(err)
	}
	return toItems(rows), nil
}

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

type repo struct{ q *db.Queries }

// RecordStarterGrant inserts the owner's grant unless it has one.
func (r repo) RecordStarterGrant(ctx context.Context, owner kernel.CharacterID) (bool, error) {
	n, err := r.q.RecordStarterGrant(ctx, uuid.UUID(owner))
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
	items := toItems(rows)
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
	return toItem(row), nil
}

// Insert adds an inventory row.
func (r repo) Insert(ctx context.Context, it model.Item) error {
	tab, slot := string(it.Tab), int32(it.Slot) //nolint:gosec // slots are small
	return r.q.InsertItem(ctx, db.InsertItemParams{
		ID: uuid.UUID(it.ID), TemplateID: it.TemplateID, OwnerID: uuid.UUID(it.Owner), Tab: &tab, Slot: &slot,
		Quantity: int32(it.Quantity), //nolint:gosec // quantities are small
	})
}

// SetQuantity changes a quantity.
func (r repo) SetQuantity(ctx context.Context, id kernel.ItemID, quantity int) error {
	n, err := r.q.SetQuantity(ctx, db.SetQuantityParams{ID: uuid.UUID(id), Quantity: int32(quantity)}) //nolint:gosec // quantities are small
	return dbx.RowsOrNotFound(n, err)
}

// SetSlot moves an item to a slot.
func (r repo) SetSlot(ctx context.Context, id kernel.ItemID, tab gamedata.Tab, slot int) error {
	t, s := string(tab), int32(slot) //nolint:gosec // slots are small
	n, err := r.q.SetSlot(ctx, db.SetSlotParams{ID: uuid.UUID(id), Tab: &t, Slot: &s})
	return dbx.RowsOrNotFound(n, err)
}

// Destroy marks an item destroyed with quantity 0 rather than deleting the row.
func (r repo) Destroy(ctx context.Context, id kernel.ItemID) error {
	n, err := r.q.DestroyItem(ctx, uuid.UUID(id))
	return dbx.RowsOrNotFound(n, err)
}

type eventRepo struct{ q *db.Queries }

// Append adds one line to the item history.
func (r eventRepo) Append(ctx context.Context, e model.Event) error {
	params := db.InsertItemEventParams{
		ItemID: uuid.UUID(e.ItemID), Kind: string(e.Kind), QuantityDelta: int32(e.QuantityDelta), //nolint:gosec // quantities are small
		CauseType: string(e.Cause.Type), OccurredAt: e.OccurredAt,
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

func toItems(rows []db.Item) []model.Item {
	out := make([]model.Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, toItem(r))
	}
	return out
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

// failures maps constraint errors and timeouts to the model's; others pass through so retries still see 40001.
var failures = dbx.Errors{
	NotFound: model.ErrNotFound,
	TimedOut: model.ErrTimedOut,
	Constraints: map[string]error{
		constraintSlot:     model.ErrSlotTaken,
		constraintQuantity: model.ErrBadQuantity,
	},
}
