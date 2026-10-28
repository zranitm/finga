// Package clients are the gateway's adapters for the services it calls in
// terms other than their generated clients: the account service as
// pkg/auth's Guard, which the REST handler and the gateway check tokens
// with.
package clients

import (
	"context"

	"github.com/google/uuid"

	accountv1 "finga/api/gen/account/v1"
	"finga/pkg/auth"
)

// Guard is the REST handler's and the gateway's auth.Guard over the account
// service. Failures pass through as they came, with their reason.
type Guard struct {
	c accountv1.AccountServiceClient
}

// NewGuard adapts the account client.
func NewGuard(c accountv1.AccountServiceClient) Guard { return Guard{c: c} }

// Validate resolves a token to its principal.
func (g Guard) Validate(ctx context.Context, token string) (auth.Principal, error) {
	res, err := g.c.Validate(ctx, &accountv1.ValidateRequest{Token: token})
	if err != nil {
		return auth.Principal{}, err
	}
	account, err := uuid.Parse(res.GetAccountId())
	if err != nil {
		return auth.Principal{}, err
	}
	session, err := uuid.Parse(res.GetSessionId())
	if err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{AccountID: account, SessionID: session}, nil
}

// Revoke ends one session.
func (g Guard) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: sessionID.String()})
	return err
}

// Rotate issues a session's next token.
func (g Guard) Rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	res, err := g.c.Rotate(ctx, &accountv1.RotateRequest{SessionId: sessionID.String()})
	return res.GetToken(), err
}

// Check reports whether a session is still active.
func (g Guard) Check(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Check(ctx, &accountv1.CheckRequest{SessionId: sessionID.String()})
	return err
}

// RevokeAll ends every session of an account.
func (g Guard) RevokeAll(ctx context.Context, accountID uuid.UUID) error {
	_, err := g.c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: accountID.String()})
	return err
}
