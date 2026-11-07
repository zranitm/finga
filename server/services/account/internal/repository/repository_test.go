package repository

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/pkg/pgtest"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
	"finga/services/account/migrations"
)

// These run against real Postgres through the store, so errors arrive classified.

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, pgtest.Migrations(migrations.FS, migrations.Table)) // the account service's set only
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func newAccount(name string) model.NewAccount {
	return model.NewAccount{ID: kernel.NewAccountID(), Username: name, PasswordHash: "$argon2id$fake", Settings: model.DefaultSettings()}
}

// insertAccount returns the account's ID and its created_at.
func insertAccount(t *testing.T, store *Postgres, name string) (kernel.AccountID, time.Time) {
	t.Helper()
	acc := newAccount(name)
	var created time.Time
	err := store.InTx(context.Background(), func(uow application.UnitOfWork) error {
		var err error
		created, err = uow.Accounts().Insert(context.Background(), acc)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return acc.ID, created
}

func hashOf(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

func TestAccounts(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)

	t.Run("insert and find by username, ignoring letter case", func(t *testing.T) {
		id, created := insertAccount(t, store, "Alice")
		if created.IsZero() {
			t.Fatal("no created_at from the insert")
		}
		creds, err := store.FindByUsername(ctx, "aLiCe")
		if err != nil {
			t.Fatal(err)
		}
		if creds.Account.ID != id || creds.Account.Username != "Alice" || creds.PasswordHash != "$argon2id$fake" || creds.Account.Settings != model.DefaultSettings() || !creds.Account.CreatedAt.Equal(created) {
			t.Fatalf("creds = %+v", creds)
		}
	})

	t.Run("a duplicate username in different letter case gives ErrUsernameTaken", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			_, err := uow.Accounts().Insert(ctx, newAccount("ALICE"))
			return err
		})
		if !errors.Is(err, model.ErrUsernameTaken) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("an unknown username gives ErrNotFound", func(t *testing.T) {
		_, err := store.FindByUsername(ctx, "nobody")
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("settings are read and written", func(t *testing.T) {
		id, _ := insertAccount(t, store, "bob")
		want := model.Settings{ControlPreset: model.PresetWASD}
		err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Accounts().UpdateSettings(ctx, id, want) })
		if err != nil {
			t.Fatal(err)
		}
		got, err := store.Settings(ctx, id)
		if err != nil || got != want {
			t.Fatalf("settings = %+v, %v", got, err)
		}
		if _, err := store.Settings(ctx, kernel.NewAccountID()); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("unknown account: %v", err)
		}
		err = store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Accounts().UpdateSettings(ctx, kernel.NewAccountID(), want)
		})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("update of an unknown account: %v", err)
		}
	})

	t.Run("a hash is replaced only while it is still the one verified", func(t *testing.T) {
		id, _ := insertAccount(t, store, "dave")
		for _, swap := range [][2]string{{"$argon2id$fake", "$argon2id$upgraded"}, {"$argon2id$fake", "$argon2id$stale"}} {
			if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Accounts().ReplaceHash(ctx, id, swap[0], swap[1]) }); err != nil {
				t.Fatal(err)
			}
		}
		creds, err := store.FindByUsername(ctx, "dave")
		if err != nil || creds.PasswordHash != "$argon2id$upgraded" {
			t.Fatalf("hash %q (%v), want the first swap's and not the second's", creds.PasswordHash, err)
		}
	})

	t.Run("locking an account row", func(t *testing.T) {
		id, _ := insertAccount(t, store, "carol")
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Accounts().Lock(ctx, id) }); err != nil {
			t.Fatal(err)
		}
		err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Accounts().Lock(ctx, kernel.NewAccountID()) })
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("lock of an unknown account: %v", err)
		}
	})
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	acc, _ := insertAccount(t, store, "dave")
	session := func(token string) model.Session {
		return model.Session{ID: kernel.NewSessionID(), AccountID: acc, TokenHash: hashOf(token), ExpiresAt: t0.Add(time.Hour)}
	}

	t.Run("insert and find an active session by token hash", func(t *testing.T) {
		s := session("t1")
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Sessions().Insert(ctx, s) }); err != nil {
			t.Fatal(err)
		}
		got, err := store.FindActiveSession(ctx, hashOf("t1"))
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != s.ID || got.AccountID != acc || !got.ExpiresAt.Equal(s.ExpiresAt) || got.RevokedAt != nil || got.CreatedAt.IsZero() {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("an unknown token hash gives ErrNotFound", func(t *testing.T) {
		if _, err := store.FindActiveSession(ctx, hashOf("nope")); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a second active session is rejected by the partial unique index as ErrSessionRace", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Sessions().Insert(ctx, session("t2")) })
		if !errors.Is(err, model.ErrSessionRace) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("revoking every session of an account leaves none active", func(t *testing.T) {
		err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Sessions().RevokeAll(ctx, acc, t0.Add(time.Minute)) })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.FindActiveSession(ctx, hashOf("t1")); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("t1 still active: %v", err)
		}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Sessions().Insert(ctx, session("t3")) }); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revoking one session by ID", func(t *testing.T) {
		got, err := store.FindActiveSession(ctx, hashOf("t3"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Sessions().Revoke(ctx, got.ID, t0.Add(2*time.Minute))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.FindActiveSession(ctx, hashOf("t3")); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("t3 still active: %v", err)
		}
		// Revoking again, or an unknown session, is not an error.
		if err := store.InTx(ctx, func(uow application.UnitOfWork) error { return uow.Sessions().Revoke(ctx, got.ID, t0) }); err != nil {
			t.Fatalf("second revoke: %v", err)
		}
	})
}

// A session goes once it has been over, by revocation or expiry, since before the cutoff; limit at a time.
func TestDeleteEndedSessions(t *testing.T) {
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	cutoff := t0
	tests := []struct {
		name             string
		expires, revoked time.Time // a zero revoked leaves the session unrevoked
		gone             bool
	}{
		{"revoked before the cutoff", cutoff.Add(6 * 24 * time.Hour), cutoff.Add(-time.Hour), true},
		{"expired before the cutoff", cutoff.Add(-time.Hour), time.Time{}, true},
		{"expired before the cutoff, revoked after", cutoff.Add(-time.Hour), cutoff.Add(time.Hour), true},
		{"revoked after the cutoff", cutoff.Add(6 * 24 * time.Hour), cutoff.Add(time.Hour), false},
		{"expired after the cutoff", cutoff.Add(time.Hour), time.Time{}, false},
		{"active", cutoff.Add(40 * 24 * time.Hour), time.Time{}, false},
	}
	ids := make([]kernel.SessionID, len(tests))
	for i, tt := range tests {
		acc, _ := insertAccount(t, store, fmt.Sprintf("user%d", i))
		ids[i] = kernel.NewSessionID()
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			if err := uow.Sessions().Insert(ctx, model.Session{ID: ids[i], AccountID: acc, TokenHash: hashOf(tt.name), ExpiresAt: tt.expires}); err != nil {
				return err
			}
			if tt.revoked.IsZero() {
				return nil
			}
			return uow.Sessions().Revoke(ctx, ids[i], tt.revoked)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	deleted := func() int64 {
		t.Helper()
		var n int64
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			var err error
			n, err = uow.Sessions().DeleteEnded(ctx, cutoff, 2)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if first, second := deleted(), deleted(); first != 2 || second != 1 {
		t.Fatalf("batches of 2 deleted %d then %d, want 2 then 1", first, second)
	}
	for i, tt := range tests {
		_, err := store.FindSession(ctx, ids[i])
		if gone := errors.Is(err, model.ErrNotFound); gone != tt.gone {
			t.Errorf("%s: gone %v (%v), want %v", tt.name, gone, err, tt.gone)
		}
	}
}

// A session another transaction holds, such as another node's prune, is skipped, not waited for.
func TestDeleteEndedSessionsSkipsOneAnotherTransactionHolds(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	store := NewPostgres(pool, nil)
	ids := []kernel.SessionID{kernel.NewSessionID(), kernel.NewSessionID()}
	for i, id := range ids {
		acc, _ := insertAccount(t, store, fmt.Sprintf("held%d", i))
		err := store.InTx(ctx, func(uow application.UnitOfWork) error {
			return uow.Sessions().Insert(ctx, model.Session{ID: id, AccountID: acc, TokenHash: hashOf(id.String()), ExpiresAt: t0.Add(-time.Hour)})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM sessions WHERE id = $1 FOR UPDATE`, ids[0].String()); err != nil {
		t.Fatal(err)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var n int64
	err = store.InTx(shortCtx, func(uow application.UnitOfWork) error {
		var err error
		n, err = uow.Sessions().DeleteEnded(shortCtx, t0, 10)
		return err
	})
	if err != nil || n != 1 {
		t.Fatalf("deleted %d (%v); want the free session gone and the held one skipped", n, err)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // errors.Is target; nil means the error passes through unchanged
	}{
		{"nil", nil, nil},
		{"no rows", noRows(), model.ErrNotFound},
		{"username unique constraint", constraint("accounts_username_key"), model.ErrUsernameTaken},
		{"one active session index", constraint("one_active_session"), model.ErrSessionRace},
		{"another constraint passes through", constraint("characters_name_key"), nil},
		{"a serialization failure past the retries", pgCode("40001"), model.ErrTimedOut},
		{"too many connections", pgCode("53300"), model.ErrUnavailable},
		{"a lock timeout", pgCode("55P03"), model.ErrTimedOut},
		{"a statement timeout", pgCode("57014"), model.ErrTimedOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failures.Classify(tt.err)
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
