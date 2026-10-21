package api_test

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/model"
	"finga/pkg/auth"
)

// The fakes return what the test tells them to; a nil function means the
// endpoint under test must not have reached that method.

type fakeAccounts struct {
	register       func(ctx context.Context, in account.RegisterInput) (account.Account, error)
	login          func(ctx context.Context, in account.LoginInput) (string, error)
	settings       func(ctx context.Context, id model.AccountID) (account.Settings, error)
	updateSettings func(ctx context.Context, id model.AccountID, s account.Settings) error
}

func (f *fakeAccounts) Register(ctx context.Context, in account.RegisterInput) (account.Account, error) {
	if f.register == nil {
		panic("unexpected Register")
	}
	return f.register(ctx, in)
}

func (f *fakeAccounts) Login(ctx context.Context, in account.LoginInput) (string, error) {
	if f.login == nil {
		panic("unexpected Login")
	}
	return f.login(ctx, in)
}

func (f *fakeAccounts) Settings(ctx context.Context, id model.AccountID) (account.Settings, error) {
	if f.settings == nil {
		panic("unexpected Settings")
	}
	return f.settings(ctx, id)
}

func (f *fakeAccounts) UpdateSettings(ctx context.Context, id model.AccountID, s account.Settings) error {
	if f.updateSettings == nil {
		panic("unexpected UpdateSettings")
	}
	return f.updateSettings(ctx, id, s)
}

type fakeCharacters struct {
	create func(ctx context.Context, owner model.AccountID, in character.CreateInput) (character.Character, error)
	list   func(ctx context.Context, owner model.AccountID) ([]character.Character, error)
}

func (f *fakeCharacters) Create(ctx context.Context, owner model.AccountID, in character.CreateInput) (character.Character, error) {
	if f.create == nil {
		panic("unexpected Create")
	}
	return f.create(ctx, owner, in)
}

func (f *fakeCharacters) List(ctx context.Context, owner model.AccountID) ([]character.Character, error) {
	if f.list == nil {
		panic("unexpected List")
	}
	return f.list(ctx, owner)
}

// fakeSessions knows one good token.
type fakeSessions struct {
	token     string
	principal auth.Principal
	revoked   []uuid.UUID
	validate  func(token string) (auth.Principal, error) // overrides the default when set
}

func (f *fakeSessions) Issue(context.Context, uuid.UUID) (string, error) {
	return "", errors.New("not used by the REST handler")
}

func (f *fakeSessions) Validate(_ context.Context, token string) (auth.Principal, error) {
	if f.validate != nil {
		return f.validate(token)
	}
	if token == f.token {
		return f.principal, nil
	}
	return auth.Principal{}, account.ErrUnauthenticated
}

func (f *fakeSessions) Revoke(_ context.Context, id uuid.UUID) error {
	f.revoked = append(f.revoked, id)
	return nil
}
