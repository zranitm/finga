// Package clients adapts the services' gRPC clients to the application's ports.
package clients

import (
	"context"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// Guard is the lobby's, the send flow's and the push flow's session ports over the account service.
type Guard struct {
	c accountv1.AccountServiceClient
}

// NewGuard adapts the account client.
func NewGuard(c accountv1.AccountServiceClient) Guard { return Guard{c: c} }

// Validate resolves a token to its principal.
func (g Guard) Validate(ctx context.Context, token string) (model.Principal, error) {
	res, err := g.c.Validate(ctx, &accountv1.ValidateRequest{Token: token})
	if err != nil {
		return model.Principal{}, errmap.Wrap(err)
	}
	account, err := kernel.ParseAccountID(res.GetAccountId())
	if err != nil {
		return model.Principal{}, errmap.Wrap(err)
	}
	session, err := kernel.ParseSessionID(res.GetSessionId())
	if err != nil {
		return model.Principal{}, errmap.Wrap(err)
	}
	return model.Principal{AccountID: account, SessionID: session}, nil
}

// Revoke ends one session.
func (g Guard) Revoke(ctx context.Context, sessionID kernel.SessionID) error {
	_, err := g.c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: sessionID.String()})
	return errmap.Wrap(err)
}

// Rotate issues a session's next token.
func (g Guard) Rotate(ctx context.Context, sessionID kernel.SessionID) (string, error) {
	res, err := g.c.Rotate(ctx, &accountv1.RotateRequest{SessionId: sessionID.String()})
	return res.GetToken(), errmap.Wrap(err)
}

// Check reports whether a session is still active.
func (g Guard) Check(ctx context.Context, sessionID kernel.SessionID) error {
	_, err := g.c.Check(ctx, &accountv1.CheckRequest{SessionId: sessionID.String()})
	return errmap.Wrap(err)
}

// RevokeAll ends every session of an account.
func (g Guard) RevokeAll(ctx context.Context, accountID kernel.AccountID) error {
	_, err := g.c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: accountID.String()})
	return errmap.Wrap(err)
}
