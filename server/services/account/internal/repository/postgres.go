// Package repository is the account service's storage (Low-level design,
// page 11): the Postgres store behind application.Store, its unit of work,
// and classify, over the accounts and sessions tables the service owns.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
	"finga/services/account/internal/repository/internal/db"
)

// Postgres is the service's store: plain reads on the pool, InTx for writes,
// and classify at every boundary so callers see the model's errors.
type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewPostgres builds the store over a pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool, q: db.New(pool)}
}

// InTx runs fn inside one transaction with a unit of work bound to it.
// fn may run more than once (dbx.InTx retries), so it must have no side
// effects outside the transaction.
func (p *Postgres) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		return fn(unitOfWork{q: db.New(tx)})
	})
	return classify(err)
}

// FindByUsername loads an account and its password hash; username is
// matched ignoring letter case (citext).
func (p *Postgres) FindByUsername(ctx context.Context, username string) (model.Credentials, error) {
	row, err := p.q.FindAccountByUsername(ctx, username)
	if err != nil {
		return model.Credentials{}, classify(err)
	}
	var settings model.Settings
	if err := json.Unmarshal(row.Settings, &settings); err != nil {
		return model.Credentials{}, fmt.Errorf("account %s: settings: %w", row.ID, err)
	}
	return model.Credentials{
		Account:      model.Account{ID: kernel.AccountID(row.ID), Username: row.Username, Settings: settings, CreatedAt: row.CreatedAt},
		PasswordHash: row.PasswordHash,
	}, nil
}

// FindActiveSession loads the unrevoked session with this token hash.
func (p *Postgres) FindActiveSession(ctx context.Context, tokenHash []byte) (model.Session, error) {
	row, err := p.q.FindActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return model.Session{}, classify(err)
	}
	return model.Session{
		ID: kernel.SessionID(row.ID), AccountID: kernel.AccountID(row.AccountID), TokenHash: row.TokenHash,
		PreviousTokenHash: row.PreviousTokenHash, RotatedAt: row.RotatedAt,
		CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt,
	}, nil
}

// Settings reads an account's settings.
func (p *Postgres) Settings(ctx context.Context, id kernel.AccountID) (model.Settings, error) {
	raw, err := p.q.GetSettings(ctx, uuid.UUID(id))
	if err != nil {
		return model.Settings{}, classify(err)
	}
	var s model.Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return model.Settings{}, fmt.Errorf("account %s: settings: %w", id, err)
	}
	return s, nil
}

type unitOfWork struct{ q *db.Queries }

// Accounts is the account repository bound to the transaction.
func (u unitOfWork) Accounts() application.Repository { return accountRepo(u) }

// Sessions is the session repository bound to the transaction.
func (u unitOfWork) Sessions() application.SessionRepository { return sessionRepo(u) }

type accountRepo struct{ q *db.Queries }

// Insert adds an account row.
func (r accountRepo) Insert(ctx context.Context, a model.NewAccount) error {
	settings, err := json.Marshal(a.Settings)
	if err != nil {
		return err
	}
	return r.q.InsertAccount(ctx, db.InsertAccountParams{
		ID: uuid.UUID(a.ID), Username: a.Username, PasswordHash: a.PasswordHash, Settings: settings, CreatedAt: a.CreatedAt,
	})
}

// Lock takes the account row lock; no row is pgx.ErrNoRows.
func (r accountRepo) Lock(ctx context.Context, id kernel.AccountID) error {
	_, err := r.q.LockAccount(ctx, uuid.UUID(id))
	return err
}

// UpdateSettings replaces the settings JSON; no row is pgx.ErrNoRows.
func (r accountRepo) UpdateSettings(ctx context.Context, id kernel.AccountID, s model.Settings) error {
	settings, err := json.Marshal(s)
	if err != nil {
		return err
	}
	n, err := r.q.UpdateSettings(ctx, db.UpdateSettingsParams{ID: uuid.UUID(id), Settings: settings})
	if err != nil {
		return err
	}
	if n == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

type sessionRepo struct{ q *db.Queries }

// Insert adds a session row.
func (r sessionRepo) Insert(ctx context.Context, s model.Session) error {
	return r.q.InsertSession(ctx, db.InsertSessionParams{
		ID: uuid.UUID(s.ID), AccountID: uuid.UUID(s.AccountID), TokenHash: s.TokenHash, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt,
	})
}

// RevokeAll ends every active session of the account.
func (r sessionRepo) RevokeAll(ctx context.Context, accountID kernel.AccountID, at time.Time) error {
	_, err := r.q.RevokeAccountSessions(ctx, db.RevokeAccountSessionsParams{AccountID: uuid.UUID(accountID), RevokedAt: &at})
	return err
}

// Revoke ends one session. Zero rows is not an error: the session is ended
// either way.
func (r sessionRepo) Revoke(ctx context.Context, id kernel.SessionID, at time.Time) error {
	_, err := r.q.RevokeSession(ctx, db.RevokeSessionParams{ID: uuid.UUID(id), RevokedAt: &at})
	return err
}

// Constraint names from the migration.
const (
	constraintUsername      = "accounts_username_key"
	constraintActiveSession = "one_active_session"
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
	case errors.As(err, &pgErr) && pgErr.ConstraintName == constraintUsername:
		return fmt.Errorf("%w: %w", model.ErrUsernameTaken, err)
	case errors.As(err, &pgErr) && pgErr.ConstraintName == constraintActiveSession:
		return fmt.Errorf("%w: %w", model.ErrSessionRace, err)
	default:
		return err
	}
}

// FindSession loads a session by ID, revoked or not.
func (p *Postgres) FindSession(ctx context.Context, id kernel.SessionID) (model.Session, error) {
	row, err := p.q.FindSessionByID(ctx, uuid.UUID(id))
	if err != nil {
		return model.Session{}, classify(err)
	}
	return model.Session{
		ID: kernel.SessionID(row.ID), AccountID: kernel.AccountID(row.AccountID), TokenHash: row.TokenHash,
		PreviousTokenHash: row.PreviousTokenHash, RotatedAt: row.RotatedAt,
		CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt,
	}, nil
}

// Rotate retires the current token and installs a new hash.
func (r sessionRepo) Rotate(ctx context.Context, id kernel.SessionID, newHash []byte, at time.Time) error {
	n, err := r.q.RotateSessionToken(ctx, db.RotateSessionTokenParams{ID: uuid.UUID(id), TokenHash: newHash, RotatedAt: &at})
	if err != nil {
		return classify(err)
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ConsumePrevious ends the grace for the previous token.
func (r sessionRepo) ConsumePrevious(ctx context.Context, id kernel.SessionID) error {
	_, err := r.q.ConsumePreviousToken(ctx, uuid.UUID(id))
	return classify(err)
}

// Extend moves the expiry.
func (r sessionRepo) Extend(ctx context.Context, id kernel.SessionID, until time.Time) error {
	_, err := r.q.ExtendSession(ctx, db.ExtendSessionParams{ID: uuid.UUID(id), ExpiresAt: until})
	return classify(err)
}
