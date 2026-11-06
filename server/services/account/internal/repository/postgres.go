// Package repository is the account service's Postgres store behind application.Store.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/kernel"
	"finga/pkg/dbx"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
	"finga/services/account/internal/repository/internal/db"
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
		return fn(unitOfWork{q: db.New(tx)})
	}, dbx.Observed(p.obs))
	return failures.Classify(err)
}

// FindByUsername loads an account and its password hash, matching the username case-insensitively.
func (p *Postgres) FindByUsername(ctx context.Context, username string) (model.Credentials, error) {
	row, err := p.q.FindAccountByUsername(ctx, username)
	if err != nil {
		return model.Credentials{}, failures.Classify(err)
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
		return model.Session{}, failures.Classify(err)
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
		return model.Settings{}, failures.Classify(err)
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

// Insert adds an account row and returns the created_at the database gave it.
func (r accountRepo) Insert(ctx context.Context, a model.NewAccount) (time.Time, error) {
	settings, err := json.Marshal(a.Settings)
	if err != nil {
		return time.Time{}, err
	}
	return r.q.InsertAccount(ctx, db.InsertAccountParams{
		ID: uuid.UUID(a.ID), Username: a.Username, PasswordHash: a.PasswordHash, Settings: settings,
	})
}

// Lock takes the account row lock.
func (r accountRepo) Lock(ctx context.Context, id kernel.AccountID) error {
	_, err := r.q.LockAccount(ctx, uuid.UUID(id))
	return err
}

// UpdateSettings replaces the settings JSON.
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

// ReplaceHash swaps the password hash while it is still old; zero rows means it changed since, which is fine.
func (r accountRepo) ReplaceHash(ctx context.Context, id kernel.AccountID, old, hash string) error {
	_, err := r.q.ReplacePasswordHash(ctx, db.ReplacePasswordHashParams{ID: uuid.UUID(id), OldHash: old, NewHash: hash})
	return err
}

type sessionRepo struct{ q *db.Queries }

// Insert adds a session row.
func (r sessionRepo) Insert(ctx context.Context, s model.Session) error {
	return r.q.InsertSession(ctx, db.InsertSessionParams{
		ID: uuid.UUID(s.ID), AccountID: uuid.UUID(s.AccountID), TokenHash: s.TokenHash, ExpiresAt: s.ExpiresAt,
	})
}

// RevokeAll ends every active session of the account.
func (r sessionRepo) RevokeAll(ctx context.Context, accountID kernel.AccountID, at time.Time) error {
	_, err := r.q.RevokeAccountSessions(ctx, db.RevokeAccountSessionsParams{AccountID: uuid.UUID(accountID), RevokedAt: &at})
	return err
}

// Revoke ends one session; zero rows is fine, the session is ended either way.
func (r sessionRepo) Revoke(ctx context.Context, id kernel.SessionID, at time.Time) error {
	_, err := r.q.RevokeSession(ctx, db.RevokeSessionParams{ID: uuid.UUID(id), RevokedAt: &at})
	return err
}

const (
	constraintUsername      = "accounts_username_key"
	constraintActiveSession = "one_active_session"
)

var failures = dbx.Errors{
	NotFound: model.ErrNotFound,
	TimedOut: model.ErrTimedOut,
	Constraints: map[string]error{
		constraintUsername:      model.ErrUsernameTaken,
		constraintActiveSession: model.ErrSessionRace,
	},
}

// FindSession loads a session by ID, revoked or not.
func (p *Postgres) FindSession(ctx context.Context, id kernel.SessionID) (model.Session, error) {
	row, err := p.q.FindSessionByID(ctx, uuid.UUID(id))
	if err != nil {
		return model.Session{}, failures.Classify(err)
	}
	return sessionOf(row), nil
}

func sessionOf(row db.FindSessionByIDRow) model.Session {
	return model.Session{
		ID: kernel.SessionID(row.ID), AccountID: kernel.AccountID(row.AccountID), TokenHash: row.TokenHash,
		PreviousTokenHash: row.PreviousTokenHash, RotatedAt: row.RotatedAt,
		CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt,
	}
}

// Rotate retires the current token and installs a new hash.
func (r sessionRepo) Rotate(ctx context.Context, id kernel.SessionID, newHash []byte, at time.Time) error {
	n, err := r.q.RotateSessionToken(ctx, db.RotateSessionTokenParams{ID: uuid.UUID(id), TokenHash: newHash, RotatedAt: &at})
	if err != nil {
		return failures.Classify(err)
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// Lock reads the session FOR UPDATE.
func (r sessionRepo) Lock(ctx context.Context, id kernel.SessionID) (model.Session, error) {
	row, err := r.q.LockSession(ctx, uuid.UUID(id))
	if err != nil {
		return model.Session{}, failures.Classify(err)
	}
	return sessionOf(db.FindSessionByIDRow(row)), nil
}

// ConsumePrevious ends the grace for the previous token.
func (r sessionRepo) ConsumePrevious(ctx context.Context, id kernel.SessionID) error {
	_, err := r.q.ConsumePreviousToken(ctx, uuid.UUID(id))
	return failures.Classify(err)
}

// DeleteEnded deletes the sessions over since before, oldest first, limit at a time.
func (r sessionRepo) DeleteEnded(ctx context.Context, before time.Time, limit int) (int64, error) {
	n, err := r.q.DeleteEndedSessions(ctx, db.DeleteEndedSessionsParams{EndedBefore: before, MaxRows: int32(limit)}) //nolint:gosec // a configured batch
	return n, failures.Classify(err)
}

// Extend moves the expiry.
func (r sessionRepo) Extend(ctx context.Context, id kernel.SessionID, until time.Time) error {
	_, err := r.q.ExtendSession(ctx, db.ExtendSessionParams{ID: uuid.UUID(id), ExpiresAt: until})
	return failures.Classify(err)
}
