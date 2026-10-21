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
	// Validate resolves a token to its principal, failing for unknown,
	// expired and revoked tokens alike.
	Validate(ctx context.Context, token string) (Principal, error)
	// Revoke ends one session. Revoking an ended session is not an error.
	Revoke(ctx context.Context, sessionID uuid.UUID) error
}
