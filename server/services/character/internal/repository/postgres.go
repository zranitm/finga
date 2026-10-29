// Package repository is the character service's storage (Low-level design,
// page 11): the Postgres store behind application.Store, its unit of work,
// and classify, over the characters table the service owns.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
	"finga/services/character/internal/repository/internal/db"
)

// Postgres is the service's store.
type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewPostgres builds the store over a pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool)}
}

// InTx runs fn inside one transaction with a unit of work bound to it.
// fn may run more than once, so it must have no side effects outside the
// transaction.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(unitOfWork{q: db.New(tx)})
	})
	return classify(err)
}

// List is the account's characters, oldest first.
func (p *Postgres) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	rows, err := p.q.ListCharactersByAccount(ctx, uuid.UUID(account))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]model.Character, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCharacter(row))
	}
	return out, nil
}

// Load is one character, only if the account owns it; otherwise ErrNotFound.
func (p *Postgres) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	row, err := p.q.GetOwnedCharacter(ctx, db.GetOwnedCharacterParams{ID: uuid.UUID(id), AccountID: uuid.UUID(account)})
	if err != nil {
		return model.Character{}, classify(err)
	}
	return toCharacter(row), nil
}

func toCharacter(r db.Character) model.Character {
	return model.Character{
		ID: kernel.CharacterID(r.ID), AccountID: kernel.AccountID(r.AccountID), Name: r.Name, Class: kernel.Class(r.Class),
		Level: int(r.Level), Exp: r.Exp,
		Stats:         gamedata.Stats{STR: int(r.Strength), DEX: int(r.Dexterity), INT: int(r.Intelligence), AGI: int(r.Agility), VIT: int(r.Vitality)},
		UnspentPoints: int(r.UnspentPoints), HP: int(r.Hp), MP: int(r.Mp),
		MapID: r.MapID, X: float64(r.PosX), Y: float64(r.PosY), UpdatedAt: r.UpdatedAt,
	}
}

type unitOfWork struct {
	q *db.Queries
}

// Characters is the character repository bound to the transaction.
func (u unitOfWork) Characters() application.Repository { return repo(u) }

type repo struct{ q *db.Queries }

// Insert adds a character row.
func (r repo) Insert(ctx context.Context, c model.Character) error {
	return r.q.InsertCharacter(ctx, db.InsertCharacterParams{
		ID: uuid.UUID(c.ID), AccountID: uuid.UUID(c.AccountID), Name: c.Name, Class: string(c.Class),
		Level: int32(c.Level), Exp: c.Exp, //nolint:gosec // levels are small
		Strength: int32(c.Stats.STR), Dexterity: int32(c.Stats.DEX), Intelligence: int32(c.Stats.INT), //nolint:gosec // stats are small
		Agility: int32(c.Stats.AGI), Vitality: int32(c.Stats.VIT), UnspentPoints: int32(c.UnspentPoints), //nolint:gosec // stats are small
		Hp: int32(c.HP), Mp: int32(c.MP), MapID: c.MapID, PosX: float32(c.X), PosY: float32(c.Y), UpdatedAt: c.UpdatedAt, //nolint:gosec // HP fits
	})
}

// SaveCheckpoint updates position, HP, MP and EXP; no row is pgx.ErrNoRows.
func (r repo) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	n, err := r.q.SaveCheckpoint(ctx, db.SaveCheckpointParams{
		ID: uuid.UUID(cp.ID), MapID: cp.MapID, PosX: float32(cp.X), PosY: float32(cp.Y),
		Hp: int32(cp.HP), Mp: int32(cp.MP), Exp: cp.Exp, UpdatedAt: cp.At, //nolint:gosec // HP fits
	})
	return rowsOrNotFound(n, err)
}

// SaveLevelUp updates level, EXP, stats, HP and MP; no row is pgx.ErrNoRows.
func (r repo) SaveLevelUp(ctx context.Context, lu model.LevelUp) error {
	n, err := r.q.SaveLevelUp(ctx, db.SaveLevelUpParams{
		ID: uuid.UUID(lu.ID), Level: int32(lu.Level), Exp: lu.Exp, //nolint:gosec // levels are small
		Strength: int32(lu.Stats.STR), Dexterity: int32(lu.Stats.DEX), Intelligence: int32(lu.Stats.INT), //nolint:gosec // stats are small
		Agility: int32(lu.Stats.AGI), Vitality: int32(lu.Stats.VIT), //nolint:gosec // stats are small
		Hp: int32(lu.HP), Mp: int32(lu.MP), UpdatedAt: lu.At, //nolint:gosec // HP fits
	})
	return rowsOrNotFound(n, err)
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

const constraintName = "characters_name_key"

// classify turns database errors into the model's errors, by constraint
// name. Unknown errors pass through.
func classify(err error) error {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", model.ErrNotFound, err)
	case errors.As(err, &pgErr) && pgErr.ConstraintName == constraintName:
		return fmt.Errorf("%w: %w", model.ErrNameTaken, err)
	default:
		return err
	}
}
