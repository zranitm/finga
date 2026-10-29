package account

import (
	"context"
	"time"

	"finga/internal/kernel"
)

// Store is what the service sees of persistence: plain reads on the pool,
// and InTx for anything that writes. The service never holds a transaction.
type Store interface {
	InTx(ctx context.Context, fn func(uow UnitOfWork) error) error
	FindByUsername(ctx context.Context, username string) (Credentials, error)
	// FindActiveSession finds the unrevoked session whose current or previous token has this hash.
	FindActiveSession(ctx context.Context, tokenHash []byte) (Session, error)
	FindSession(ctx context.Context, id kernel.SessionID) (Session, error)
	Settings(ctx context.Context, id kernel.AccountID) (Settings, error)
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
	Lock(ctx context.Context, id kernel.AccountID) error
	UpdateSettings(ctx context.Context, id kernel.AccountID, s Settings) error
}

// SessionRepository writes the sessions table.
type SessionRepository interface {
	Insert(ctx context.Context, s Session) error
	RevokeAll(ctx context.Context, accountID kernel.AccountID, at time.Time) error
	Revoke(ctx context.Context, id kernel.SessionID, at time.Time) error
	// Rotate retires the current token as the previous one and sets a new hash; ErrNotFound when the session is not active.
	Rotate(ctx context.Context, id kernel.SessionID, newHash []byte, at time.Time) error
	// ConsumePrevious marks the previous token as used (the grace is over for it).
	ConsumePrevious(ctx context.Context, id kernel.SessionID) error
	Extend(ctx context.Context, id kernel.SessionID, until time.Time) error
}
