package local

import (
	"context"
	"log/slog"

	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/account"
	"finga/services/gateway/internal/model"
)

// AccountModule is the account module as the gateway calls it.
type AccountModule interface {
	Register(ctx context.Context, c account.Credentials) (account.Account, error)
	Login(ctx context.Context, c account.Credentials) (string, error)
	Validate(ctx context.Context, token string) (account.Principal, error)
	Rotate(ctx context.Context, id kernel.SessionID) (string, error)
	Check(ctx context.Context, id kernel.SessionID) error
	Revoke(ctx context.Context, id kernel.SessionID) error
	RevokeAll(ctx context.Context, id kernel.AccountID) error
	Settings(ctx context.Context, id kernel.AccountID) (account.Settings, error)
	UpdateSettings(ctx context.Context, id kernel.AccountID, s account.Settings) error
	Subscribe(gateway string) *hub.Sub[account.Event]
}

// Accounts is the application's Accounts over the account module.
type Accounts struct{ a AccountModule }

// NewAccounts adapts the account module.
func NewAccounts(a AccountModule) Accounts { return Accounts{a: a} }

// Register signs up.
func (a Accounts) Register(ctx context.Context, username, password string) (model.Account, error) {
	acc, err := a.a.Register(ctx, account.Credentials{Username: username, Password: password})
	if err != nil {
		return model.Account{}, wrap(err)
	}
	return model.Account{ID: acc.ID, Username: acc.Username, CreatedAt: acc.CreatedAt.UTC()}, nil
}

// Login opens a session.
func (a Accounts) Login(ctx context.Context, username, password string) (string, error) {
	token, err := a.a.Login(ctx, account.Credentials{Username: username, Password: password})
	return token, wrap(err)
}

// Settings are the account's settings.
func (a Accounts) Settings(ctx context.Context, id kernel.AccountID) (model.Settings, error) {
	s, err := a.a.Settings(ctx, id)
	if err != nil {
		return model.Settings{}, wrap(err)
	}
	return model.Settings{ControlPreset: s.ControlPreset, AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned}, nil
}

// UpdateSettings replaces the account's settings.
func (a Accounts) UpdateSettings(ctx context.Context, id kernel.AccountID, s model.Settings) error {
	return wrap(a.a.UpdateSettings(ctx, id, account.Settings{ControlPreset: s.ControlPreset, AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned}))
}

// Guard is the lobby's, the send flow's and the push flow's session ports over the account module.
type Guard struct{ a AccountModule }

// NewGuard adapts the account module.
func NewGuard(a AccountModule) Guard { return Guard{a: a} }

// Validate resolves a token to its principal.
func (g Guard) Validate(ctx context.Context, token string) (model.Principal, error) {
	p, err := g.a.Validate(ctx, token)
	if err != nil {
		return model.Principal{}, wrap(err)
	}
	return model.Principal{AccountID: p.AccountID, SessionID: p.SessionID}, nil
}

// Revoke ends one session.
func (g Guard) Revoke(ctx context.Context, id kernel.SessionID) error {
	return wrap(g.a.Revoke(ctx, id))
}

// Rotate issues a session's next token.
func (g Guard) Rotate(ctx context.Context, id kernel.SessionID) (string, error) {
	token, err := g.a.Rotate(ctx, id)
	return token, wrap(err)
}

// Check reports whether a session is still active.
func (g Guard) Check(ctx context.Context, id kernel.SessionID) error { return wrap(g.a.Check(ctx, id)) }

// RevokeAll ends every session of an account.
func (g Guard) RevokeAll(ctx context.Context, id kernel.AccountID) error {
	return wrap(g.a.RevokeAll(ctx, id))
}

// SessionEnds is the application's SessionEnds over the account module.
type SessionEnds struct {
	a      AccountModule
	log    *slog.Logger
	panics *panics.Recorder
}

// NewSessionEnds adapts the account module; log hears the stream's breaks, and rec a panic handling it.
func NewSessionEnds(a AccountModule, log *slog.Logger, rec *panics.Recorder) SessionEnds {
	return SessionEnds{a: a, log: log, panics: rec}
}

// FollowSessionEnds follows the ended sessions until ctx ends.
func (s SessionEnds) FollowSessionEnds(ctx context.Context, gateway string, deliver func(model.SessionEnd)) {
	hub.Follow(ctx, func() *hub.Sub[account.Event] { return s.a.Subscribe(gateway) }, nil, func(e account.Event) {
		if end, ok := sessionEnd(e); ok {
			deliver(end)
		}
	}, hub.FollowOptions{Panics: s.panics, OnBreak: func(err error) {
		s.log.WarnContext(ctx, "session stream broke; subscribing again", "err", err)
	}})
}

// sessionEnd is one event as the gateway acts on it; false for one it has no name for.
func sessionEnd(e account.Event) (model.SessionEnd, bool) {
	switch e := e.(type) {
	case account.SessionRevoked:
		return model.SessionRevoked{Session: e.Session}, true
	case account.Revoked:
		return model.AccountRevoked{Account: e.Account, NewLogin: e.Reason == account.RevokeNewLogin}, true
	}
	return nil, false
}
