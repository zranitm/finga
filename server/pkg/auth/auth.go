// Package auth is the authentication seam (TD-17): handlers and the gateway
// depend on these interfaces, the built-in account module implements them
// today, and an identity provider can replace it later without touching
// either. IDs are plain UUIDs because this package knows no domain types.
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
	// Verify returns the account ID for a correct username and password.
	// Unknown users and wrong passwords fail the same way.
	Verify(ctx context.Context, username, password string) (uuid.UUID, error)
}

// Sessions issues and checks opaque session tokens.
type Sessions interface {
	// Issue creates a session for the account and returns its token, the
	// only time the token is visible; older sessions of the account end.
	Issue(ctx context.Context, accountID uuid.UUID) (token string, err error)
	Guard
}

// Guard checks and ends sessions: what the handlers in front of the game
// need, without issuing (logging in issues). An implementation may sit
// across a network, as the gateway's does in front of the account service.
type Guard interface {
	// Validate resolves a token to its principal, failing for unknown,
	// expired and revoked tokens alike.
	Validate(ctx context.Context, token string) (Principal, error)
	// Revoke ends one session. Revoking an ended session is not an error.
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	SessionLifecycle
}

// SessionLifecycle is what step 16 added to Sessions; the gateway and the
// REST handler need all of it, so Guard embeds it.
type SessionLifecycle interface {
	// Rotate gives the session a new token; the old one stays valid once
	// within a short grace period, and any other use of it ends the session.
	Rotate(ctx context.Context, sessionID uuid.UUID) (token string, err error)
	// Check reports whether the session is still active and unexpired.
	Check(ctx context.Context, sessionID uuid.UUID) error
	// RevokeAll ends every session of the account (log out everywhere).
	RevokeAll(ctx context.Context, accountID uuid.UUID) error
}
