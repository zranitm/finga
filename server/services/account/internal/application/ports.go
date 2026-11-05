package application

import (
	"context"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/services/account/internal/model"
)

// Store is the service's persistence: plain reads on the pool, InTx for anything that writes.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	FindByUsername(ctx context.Context, username string) (model.Credentials, error)
	// FindActiveSession finds the unrevoked session whose current or previous token has this hash.
	FindActiveSession(ctx context.Context, tokenHash []byte) (model.Session, error)
	FindSession(ctx context.Context, id kernel.SessionID) (model.Session, error)
	Settings(ctx context.Context, id kernel.AccountID) (model.Settings, error)
}

// UnitOfWork is every repository bound to one transaction.
type UnitOfWork interface {
	Accounts() Repository
	Sessions() SessionRepository
}

// Repository writes the accounts table.
type Repository interface {
	// Insert returns the created_at the database gave the account.
	Insert(ctx context.Context, a model.NewAccount) (time.Time, error)
	// Lock takes the account's row lock, serialising logins per account.
	Lock(ctx context.Context, id kernel.AccountID) error
	UpdateSettings(ctx context.Context, id kernel.AccountID, s model.Settings) error
}

// SessionRepository writes the sessions table.
type SessionRepository interface {
	Insert(ctx context.Context, s model.Session) error
	RevokeAll(ctx context.Context, accountID kernel.AccountID, at time.Time) error
	Revoke(ctx context.Context, id kernel.SessionID, at time.Time) error
	// Rotate retires the current token as the previous one; ErrNotFound when the session is not active.
	Rotate(ctx context.Context, id kernel.SessionID, newHash []byte, at time.Time) error
	// Lock reads the session and holds its row until the transaction ends.
	Lock(ctx context.Context, id kernel.SessionID) (model.Session, error)
	ConsumePrevious(ctx context.Context, id kernel.SessionID) error
	Extend(ctx context.Context, id kernel.SessionID, until time.Time) error
}

// Notifier hears of ended sessions, after they commit, so their connections close; it must not block.
type Notifier interface {
	SessionRevoked(ctx context.Context, sessionID uuid.UUID, reason string)
	AccountRevoked(ctx context.Context, accountID uuid.UUID, reason string)
}
