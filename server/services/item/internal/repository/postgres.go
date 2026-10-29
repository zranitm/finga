// Package repository is the item service's storage (Low-level design, page
// 11): the Postgres store behind application.Store, its unit of work (the
// items, the item events, the outbox and idempotency keys), and classify,
// over the items and item_events tables the service owns.
package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/pkg/idempotency"
	"finga/pkg/outbox"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/internal/repository/internal/db"
)

// Postgres is the service's store: plain reads on the pool, InTx for writes,
// UnitOfWork for joining another module's transaction, and classify at
// every boundary.
type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
	now  func() time.Time
}

// NewPostgres builds the store over a pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool), now: time.Now}
}

// InTx runs fn inside one transaction with a unit of work bound to it. fn
// may run more than once, so it must have no side effects outside the
// transaction.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(p.unitOfWork(tx))
	})
	return classify(err)
}

// unitOfWork builds the store's unit of work over a transaction.
func (p *Postgres) unitOfWork(tx pgx.Tx) application.UnitOfWork {
	q := db.New(tx)
	return unitOfWork{
		items:  repo{q: q, now: p.now},
		events: eventRepo{q: q},
		outbox: outbox.NewWriter(tx),
		idem:   idem{store: idempotency.NewStore(tx)},
	}
}

// Inventory lists the owner's inventory, by tab and slot.
func (p *Postgres) Inventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) {
	rows, err := p.q.ListInventory(ctx, uuid.UUID(owner))
	if err != nil {
		return nil, classify(err)
	}
	return toItems(rows), nil
}

type unitOfWork struct {
	items  repo
	events eventRepo
	outbox outbox.Writer
	idem   idem
}

// Items is the item repository bound to the transaction.
func (u unitOfWork) Items() application.Repository { return u.items }

// Events is the event repository bound to the transaction.
func (u unitOfWork) Events() application.EventRepository { return u.events }

// Outbox is the outbox writer bound to the transaction.
func (u unitOfWork) Outbox() outbox.Writer { return u.outbox }

// Idempotency is the idempotency store bound to the transaction.
func (u unitOfWork) Idempotency() application.Idempotency { return u.idem }

type repo struct {
	q   *db.Queries
	now func() time.Time
}

// LockInventory locks the owner's inventory rows in ID order.
func (r repo) LockInventory(ctx context.Context, owner kernel.CharacterID) ([]model.Item, error) {
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
		Quantity: int32(it.Quantity), UpdatedAt: it.UpdatedAt, //nolint:gosec // quantities are small
	})
}

// SetQuantity changes a quantity; no row is pgx.ErrNoRows.
func (r repo) SetQuantity(ctx context.Context, id kernel.ItemID, quantity int) error {
	n, err := r.q.SetQuantity(ctx, db.SetQuantityParams{ID: uuid.UUID(id), Quantity: int32(quantity), UpdatedAt: r.now()}) //nolint:gosec // quantities are small
	return rowsOrNotFound(n, err)
}

// SetSlot moves an item to a slot; no row is pgx.ErrNoRows.
func (r repo) SetSlot(ctx context.Context, id kernel.ItemID, tab gamedata.Tab, slot int) error {
	t, s := string(tab), int32(slot) //nolint:gosec // slots are small
	n, err := r.q.SetSlot(ctx, db.SetSlotParams{ID: uuid.UUID(id), Tab: &t, Slot: &s, UpdatedAt: r.now()})
	return rowsOrNotFound(n, err)
}

// Destroy marks an item destroyed with quantity 0; no row is pgx.ErrNoRows.
func (r repo) Destroy(ctx context.Context, id kernel.ItemID) error {
	n, err := r.q.DestroyItem(ctx, db.DestroyItemParams{ID: uuid.UUID(id), UpdatedAt: r.now()})
	return rowsOrNotFound(n, err)
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

// idem adapts pkg/idempotency to the use cases: it hashes the request as JSON
// and stores the Result as JSON.
type idem struct{ store idempotency.Store }

// Run runs fn once per key for the account.
func (i idem) Run(ctx context.Context, owner kernel.AccountID, scope, key string, request any, fn func() (application.Result, error)) (application.Result, error) {
	reqJSON, err := json.Marshal(request)
	if err != nil {
		return application.Result{}, err
	}
	hash := sha256.Sum256(reqJSON)
	raw, err := i.store.Do(ctx, owner.String(), scope, key, hash[:], func() ([]byte, error) {
		res, err := fn()
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	})
	if err != nil {
		if errors.Is(err, idempotency.ErrKeyReused) {
			return application.Result{}, fmt.Errorf("%w: %w", model.ErrKeyReused, err)
		}
		return application.Result{}, err
	}
	var res application.Result
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res); err != nil {
			return application.Result{}, fmt.Errorf("item: stored response: %w", err)
		}
	}
	return res, nil
}

func toItems(rows []db.Item) []model.Item {
	out := make([]model.Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, toItem(r))
	}
	return out
}

func toItem(r db.Item) model.Item {
	it := model.Item{ID: kernel.ItemID(r.ID), TemplateID: r.TemplateID, Owner: kernel.CharacterID(r.OwnerID), Quantity: int(r.Quantity), UpdatedAt: r.UpdatedAt}
	if r.Tab != nil {
		it.Tab = gamedata.Tab(*r.Tab)
	}
	if r.Slot != nil {
		it.Slot = int(*r.Slot)
	}
	return it
}

func rowsOrNotFound(n int64, err error) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Constraint names from the migration.
const (
	constraintSlot     = "one_item_per_slot"
	constraintQuantity = "items_quantity_check"
)

// classify turns database errors into the model's errors, by constraint
// name. Unknown errors pass through: retries still see 40001, and handlers
// report the rest as internal.
func classify(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", model.ErrNotFound, err)
	case errors.As(err, &pgErr) && pgErr.ConstraintName == constraintSlot:
		return fmt.Errorf("%w: %w", model.ErrSlotTaken, err)
	case errors.As(err, &pgErr) && pgErr.ConstraintName == constraintQuantity:
		return fmt.Errorf("%w: %w", model.ErrBadQuantity, err)
	default:
		return err
	}
}
