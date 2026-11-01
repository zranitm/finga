// Package auth is the authentication seam; IDs are plain UUIDs because it knows no domain types.
package auth

import (
	"context"

	"github.com/google/uuid"
)

// Principal is who a validated session belongs to.
type Principal struct {
	AccountID uuid.UUID
	SessionID uuid.UUID
}

// Authenticator checks credentials.
type Authenticator interface {
	// Verify returns the account ID; unknown users and wrong passwords fail alike.
	Verify(ctx context.Context, username, password string) (uuid.UUID, error)
}

// Sessions issues and checks opaque session tokens.
type Sessions interface {
	// Issue starts a session and returns its token; the account's older sessions end.
	Issue(ctx context.Context, accountID uuid.UUID) (token string, err error)
	Guard
}

// Guard checks and ends sessions without issuing them; an implementation may be remote.
type Guard interface {
	// Validate resolves a token to its principal; unknown, expired and revoked tokens fail alike.
	Validate(ctx context.Context, token string) (Principal, error)
	// Revoke ends one session. Revoking an ended session is not an error.
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	SessionLifecycle
}

// SessionLifecycle rotates, checks and revokes sessions.
type SessionLifecycle interface {
	// Rotate issues a new token; the old one works once within a short grace, other reuse ends the session.
	Rotate(ctx context.Context, sessionID uuid.UUID) (token string, err error)
	Check(ctx context.Context, sessionID uuid.UUID) error
	RevokeAll(ctx context.Context, accountID uuid.UUID) error
}
