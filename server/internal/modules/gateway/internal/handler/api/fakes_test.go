package api_test

import (
	"context"
	"fmt"

	"finga/internal/kernel"
	"finga/modules/account"
	"finga/modules/character"
	"finga/pkg/hub"
)

// The fakes stand in for the modules behind the production adapters; a nil function means the endpoint under test must not reach it.

// fakeAccounts is the account module, which also holds the sessions: it knows one good token.
type fakeAccounts struct {
	register       func(ctx context.Context, c account.Credentials) (account.Account, error)
	login          func(ctx context.Context, c account.Credentials) (string, error)
	settings       func(ctx context.Context, id kernel.AccountID) (account.Settings, error)
	updateSettings func(ctx context.Context, id kernel.AccountID, s account.Settings) error

	token           string
	principal       account.Principal
	revoked         []kernel.SessionID
	revokedAccounts []kernel.AccountID
	validate        func(token string) (account.Principal, error) // overrides the default when set
}

func (f *fakeAccounts) Register(ctx context.Context, c account.Credentials) (account.Account, error) {
	if f.register == nil {
		panic("unexpected Register")
	}
	return f.register(ctx, c)
}

func (f *fakeAccounts) Login(ctx context.Context, c account.Credentials) (string, error) {
	if f.login == nil {
		panic("unexpected Login")
	}
	return f.login(ctx, c)
}

func (f *fakeAccounts) Settings(ctx context.Context, id kernel.AccountID) (account.Settings, error) {
	if f.settings == nil {
		panic("unexpected Settings")
	}
	return f.settings(ctx, id)
}

func (f *fakeAccounts) UpdateSettings(ctx context.Context, id kernel.AccountID, s account.Settings) error {
	if f.updateSettings == nil {
		panic("unexpected UpdateSettings")
	}
	return f.updateSettings(ctx, id, s)
}

var errUnauthenticated = fmt.Errorf("%w: no such session", kernel.ErrUnauthenticated)

func (f *fakeAccounts) Validate(_ context.Context, token string) (account.Principal, error) {
	if f.validate != nil {
		return f.validate(token)
	}
	if token == f.token {
		return f.principal, nil
	}
	return account.Principal{}, errUnauthenticated
}

func (f *fakeAccounts) Revoke(_ context.Context, id kernel.SessionID) error {
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakeAccounts) RevokeAll(_ context.Context, id kernel.AccountID) error {
	f.revokedAccounts = append(f.revokedAccounts, id)
	return nil
}

func (f *fakeAccounts) Rotate(context.Context, kernel.SessionID) (string, error) {
	panic("unexpected Rotate")
}

func (f *fakeAccounts) Check(context.Context, kernel.SessionID) error { panic("unexpected Check") }

func (f *fakeAccounts) Subscribe(string) *hub.Sub[account.Event] { panic("unexpected Subscribe") }

type fakeCharacters struct {
	create func(ctx context.Context, owner kernel.AccountID, name string) (character.Character, error)
	list   func(ctx context.Context, owner kernel.AccountID) ([]character.Character, error)
	setBar func(ctx context.Context, id kernel.CharacterID, owner kernel.AccountID, bar []string) error
}

func (f *fakeCharacters) SetSkillBar(ctx context.Context, id kernel.CharacterID, owner kernel.AccountID, bar []string) error {
	if f.setBar == nil {
		panic("unexpected SetSkillBar")
	}
	return f.setBar(ctx, id, owner, bar)
}

func (f *fakeCharacters) Create(ctx context.Context, owner kernel.AccountID, name string) (character.Character, error) {
	if f.create == nil {
		panic("unexpected Create")
	}
	return f.create(ctx, owner, name)
}

func (f *fakeCharacters) List(ctx context.Context, owner kernel.AccountID) ([]character.Character, error) {
	if f.list == nil {
		panic("unexpected List")
	}
	return f.list(ctx, owner)
}
