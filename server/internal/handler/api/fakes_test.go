package api_test

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
)

// The fakes return what the test tells them to; a nil function means the
// endpoint under test must not have reached that method.

type fakeAccounts struct {
	register       func(ctx context.Context, in *accountv1.RegisterRequest) (*accountv1.RegisterResponse, error)
	login          func(ctx context.Context, in *accountv1.LoginRequest) (*accountv1.LoginResponse, error)
	settings       func(ctx context.Context, in *accountv1.GetSettingsRequest) (*accountv1.GetSettingsResponse, error)
	updateSettings func(ctx context.Context, in *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error)
}

func (f *fakeAccounts) Register(ctx context.Context, in *accountv1.RegisterRequest, _ ...grpc.CallOption) (*accountv1.RegisterResponse, error) {
	if f.register == nil {
		panic("unexpected Register")
	}
	return f.register(ctx, in)
}

func (f *fakeAccounts) Login(ctx context.Context, in *accountv1.LoginRequest, _ ...grpc.CallOption) (*accountv1.LoginResponse, error) {
	if f.login == nil {
		panic("unexpected Login")
	}
	return f.login(ctx, in)
}

func (f *fakeAccounts) GetSettings(ctx context.Context, in *accountv1.GetSettingsRequest, _ ...grpc.CallOption) (*accountv1.GetSettingsResponse, error) {
	if f.settings == nil {
		panic("unexpected GetSettings")
	}
	return f.settings(ctx, in)
}

func (f *fakeAccounts) UpdateSettings(ctx context.Context, in *accountv1.UpdateSettingsRequest, _ ...grpc.CallOption) (*accountv1.UpdateSettingsResponse, error) {
	if f.updateSettings == nil {
		panic("unexpected UpdateSettings")
	}
	return f.updateSettings(ctx, in)
}

type fakeCharacters struct {
	create func(ctx context.Context, in *characterv1.CreateRequest) (*characterv1.CreateResponse, error)
	list   func(ctx context.Context, in *characterv1.ListRequest) (*characterv1.ListResponse, error)
}

func (f *fakeCharacters) Create(ctx context.Context, in *characterv1.CreateRequest, _ ...grpc.CallOption) (*characterv1.CreateResponse, error) {
	if f.create == nil {
		panic("unexpected Create")
	}
	return f.create(ctx, in)
}

func (f *fakeCharacters) List(ctx context.Context, in *characterv1.ListRequest, _ ...grpc.CallOption) (*characterv1.ListResponse, error) {
	if f.list == nil {
		panic("unexpected List")
	}
	return f.list(ctx, in)
}

// errUnauthenticated is what the account service answers for a bad token.
var errUnauthenticated = grpcx.Error(codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED)

// fakeSessions knows one good token.
type fakeSessions struct {
	token           string
	principal       auth.Principal
	revoked         []uuid.UUID
	revokedAccounts []uuid.UUID
	validate        func(token string) (auth.Principal, error) // overrides the default when set
}

func (f *fakeSessions) Validate(_ context.Context, token string) (auth.Principal, error) {
	if f.validate != nil {
		return f.validate(token)
	}
	if token == f.token {
		return f.principal, nil
	}
	return auth.Principal{}, errUnauthenticated
}

func (f *fakeSessions) Revoke(_ context.Context, id uuid.UUID) error {
	f.revoked = append(f.revoked, id)
	return nil
}

func (f *fakeSessions) Rotate(context.Context, uuid.UUID) (string, error) {
	return "", errors.New("not used by the REST handler")
}

func (f *fakeSessions) Check(context.Context, uuid.UUID) error { return nil }

func (f *fakeSessions) RevokeAll(_ context.Context, accountID uuid.UUID) error {
	f.revokedAccounts = append(f.revokedAccounts, accountID)
	return nil
}
