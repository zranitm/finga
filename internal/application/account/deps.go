package account

import (
	"context"
	"time"

	"finga/internal/model"
)

// Store is what the service sees of persistence: plain reads on the pool,
// and InTx for anything that writes. The service never holds a transaction.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	FindByUsername(ctx context.Context, username string) (Credentials, error)
	FindActiveSession(ctx context.Context, tokenHash []byte) (Session, error)
	Settings(ctx context.Context, id model.AccountID) (Settings, error)
}

// UnitOfWork is every repository bound to one transaction.
type UnitOfWork interface {
	Accounts() Repository
	Sessions() SessionRepository
}

// Repository writes the accounts table.
type Repository interface {
	Insert(ctx context.Context, a NewAccount) error
	// Lock takes the account's row lock, serialising logins per account.
	Lock(ctx context.Context, id model.AccountID) error
	UpdateSettings(ctx context.Context, id model.AccountID, s Settings) error
}

// SessionRepository writes the sessions table.
type SessionRepository interface {
	Insert(ctx context.Context, s Session) error
	RevokeAll(ctx context.Context, accountID model.AccountID, at time.Time) error
	Revoke(ctx context.Context, id model.SessionID, at time.Time) error
}
