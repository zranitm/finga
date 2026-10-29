package application

import (
	"context"
	"errors"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/auth/password"
	"finga/services/account/internal/model"
)

// noAccounts is a store with no account in it.
type noAccounts struct{ Store }

func (noAccounts) FindByUsername(context.Context, string) (model.Credentials, error) {
	return model.Credentials{}, model.ErrNotFound
}

func (noAccounts) Settings(context.Context, kernel.AccountID) (model.Settings, error) {
	return model.Settings{}, model.ErrNotFound
}

// An unknown username is verified against a dummy hash, so it takes as long
// as a wrong password and the answer does not say which it was.
func TestUnknownUsernameVerifiesADummyHash(t *testing.T) {
	svc := NewService(noAccounts{}, Options{Hash: password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}})
	if svc.dummy == "" {
		t.Fatal("no dummy hash")
	}
	if _, err := svc.Verify(context.Background(), "nobody", "whatever"); !errors.Is(err, model.ErrUnauthenticated) {
		t.Fatalf("unknown user: %v", err)
	}
}
