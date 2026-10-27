package account

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"finga/internal/model"
	"finga/pkg/pgtest"
	"finga/services/account/migrations"
)

// These run against real Postgres through the store, so errors arrive
// classified. They are in the account package to reach ErrSessionRace.

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, migrations.Up) // the account service's set only
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func newAccount(name string) NewAccount {
	return NewAccount{ID: model.NewAccountID(), Username: name, PasswordHash: "$argon2id$fake", Settings: DefaultSettings(), CreatedAt: t0}
}

func insertAccount(t *testing.T, store *Postgres, name string) model.AccountID {
	t.Helper()
	acc := newAccount(name)
	if err := store.InTx(context.Background(), func(uow UnitOfWork) error { return uow.Accounts().Insert(context.Background(), acc) }); err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

func hashOf(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t))

	t.Run("insert and find by username, ignoring letter case", func(t *testing.T) {
		id := insertAccount(t, store, "Alice")
		creds, err := store.FindByUsername(ctx, "aLiCe")
		if err != nil {
			t.Fatal(err)
		}
		if creds.Account.ID != id || creds.Account.Username != "Alice" || creds.PasswordHash != "$argon2id$fake" || creds.Account.Settings != DefaultSettings() {
			t.Fatalf("creds = %+v", creds)
		}
	})

	t.Run("a duplicate username in different letter case gives ErrUsernameTaken", func(t *testing.T) {
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Accounts().Insert(ctx, newAccount("ALICE")) })
		if !errors.Is(err, ErrUsernameTaken) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("an unknown username gives ErrNotFound", func(t *testing.T) {
		_, err := store.FindByUsername(ctx, "nobody")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("settings are read and written", func(t *testing.T) {
		id := insertAccount(t, store, "bob")
		want := Settings{ControlPreset: PresetWASD}
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Accounts().UpdateSettings(ctx, id, want) })
		if err != nil {
			t.Fatal(err)
		}
		got, err := store.Settings(ctx, id)
		if err != nil || got != want {
			t.Fatalf("settings = %+v, %v", got, err)
		}
		if _, err := store.Settings(ctx, model.NewAccountID()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown account: %v", err)
		}
		err = store.InTx(ctx, func(uow UnitOfWork) error { return uow.Accounts().UpdateSettings(ctx, model.NewAccountID(), want) })
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("update of an unknown account: %v", err)
		}
	})

	t.Run("locking an account row", func(t *testing.T) {
		id := insertAccount(t, store, "carol")
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Accounts().Lock(ctx, id) }); err != nil {
			t.Fatal(err)
		}
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Accounts().Lock(ctx, model.NewAccountID()) })
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("lock of an unknown account: %v", err)
		}
	})
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t))
	acc := insertAccount(t, store, "dave")
	session := func(token string) Session {
		return Session{ID: model.NewSessionID(), AccountID: acc, TokenHash: hashOf(token), CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}
	}

	t.Run("insert and find an active session by token hash", func(t *testing.T) {
		s := session("t1")
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().Insert(ctx, s) }); err != nil {
			t.Fatal(err)
		}
		got, err := store.FindActiveSession(ctx, hashOf("t1"))
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != s.ID || got.AccountID != acc || !got.ExpiresAt.Equal(s.ExpiresAt) || got.RevokedAt != nil {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("an unknown token hash gives ErrNotFound", func(t *testing.T) {
		if _, err := store.FindActiveSession(ctx, hashOf("nope")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a second active session is rejected by the partial unique index as ErrSessionRace", func(t *testing.T) {
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().Insert(ctx, session("t2")) })
		if !errors.Is(err, ErrSessionRace) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("revoking every session of an account leaves none active", func(t *testing.T) {
		err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().RevokeAll(ctx, acc, t0.Add(time.Minute)) })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FindActiveSession(ctx, hashOf("t1")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("t1 still active: %v", err)
		}
		// Now a new session fits.
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().Insert(ctx, session("t3")) }); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revoking one session by ID", func(t *testing.T) {
		got, err := store.FindActiveSession(ctx, hashOf("t3"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().Revoke(ctx, got.ID, t0.Add(2*time.Minute)) }); err != nil {
			t.Fatal(err)
		}
		if _, err := store.FindActiveSession(ctx, hashOf("t3")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("t3 still active: %v", err)
		}
		// Revoking again, or an unknown session, is not an error: the end
		// state is what was asked for.
		if err := store.InTx(ctx, func(uow UnitOfWork) error { return uow.Sessions().Revoke(ctx, got.ID, t0) }); err != nil {
			t.Fatalf("second revoke: %v", err)
		}
	})
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // errors.Is target; nil means the error passes through unchanged
	}{
		{"nil", nil, nil},
		{"no rows", noRows(), ErrNotFound},
		{"username unique constraint", constraint("accounts_username_key"), ErrUsernameTaken},
		{"one active session index", constraint("one_active_session"), ErrSessionRace},
		{"another constraint passes through", constraint("characters_name_key"), nil},
		{"a serialization failure passes through for retries", pgCode("40001"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err)
			if tt.want == nil {
				if !errors.Is(got, tt.err) && got != nil {
					t.Fatalf("got %v, want %v unchanged", got, tt.err)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if !errors.Is(got, tt.err) {
				t.Fatal("the cause is no longer reachable")
			}
		})
	}
}
