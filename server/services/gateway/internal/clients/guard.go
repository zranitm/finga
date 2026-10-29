// Package clients are the gateway's adapters for the services it calls
// (Low-level design, page 11): each implements a port of the application
// over a service's generated client, turns proto into the gateway's model
// and the client protocol's messages, and hands every failure over through
// errmap.Wrap, so the use cases know what the client sees without knowing
// gRPC. The account service is also pkg/auth's Guard, which the REST API
// and the connections check tokens with.
package clients

import (
	"context"

	"github.com/google/uuid"

	accountv1 "finga/api/gen/account/v1"
	"finga/pkg/auth"
	"finga/services/gateway/internal/clients/errmap"
)

// Guard is the REST API's and the connections' auth.Guard over the account
// service. Failures are wrapped with their reason (errmap.Wrap).
type Guard struct {
	c accountv1.AccountServiceClient
}

// NewGuard adapts the account client.
func NewGuard(c accountv1.AccountServiceClient) Guard { return Guard{c: c} }

// Validate resolves a token to its principal.
func (g Guard) Validate(ctx context.Context, token string) (auth.Principal, error) {
	res, err := g.c.Validate(ctx, &accountv1.ValidateRequest{Token: token})
	if err != nil {
		return auth.Principal{}, errmap.Wrap(err)
	}
	account, err := uuid.Parse(res.GetAccountId())
	if err != nil {
		return auth.Principal{}, errmap.Wrap(err)
	}
	session, err := uuid.Parse(res.GetSessionId())
	if err != nil {
		return auth.Principal{}, errmap.Wrap(err)
	}
	return auth.Principal{AccountID: account, SessionID: session}, nil
}

// Revoke ends one session.
func (g Guard) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: sessionID.String()})
	return errmap.Wrap(err)
}

// Rotate issues a session's next token.
func (g Guard) Rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	res, err := g.c.Rotate(ctx, &accountv1.RotateRequest{SessionId: sessionID.String()})
	return res.GetToken(), errmap.Wrap(err)
}

// Check reports whether a session is still active.
func (g Guard) Check(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Check(ctx, &accountv1.CheckRequest{SessionId: sessionID.String()})
	return errmap.Wrap(err)
}

// RevokeAll ends every session of an account.
func (g Guard) RevokeAll(ctx context.Context, accountID uuid.UUID) error {
	_, err := g.c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: accountID.String()})
	return errmap.Wrap(err)
}
