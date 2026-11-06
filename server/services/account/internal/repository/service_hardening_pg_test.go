package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/kernel"
	"finga/pkg/auth/password"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
)

type notices struct {
	mu             sync.Mutex
	sessions       []uuid.UUID
	reasons        []string
	accounts       []uuid.UUID
	accountReasons []string
}

func (h *notices) SessionRevoked(_ context.Context, id uuid.UUID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, id)
	h.reasons = append(h.reasons, reason)
}

func (h *notices) AccountRevoked(_ context.Context, id uuid.UUID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accounts = append(h.accounts, id)
	h.accountReasons = append(h.accountReasons, reason)
}

var cheapHash = password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}

// underSynctest runs f on fake time against a fresh database, opening the pool inside the bubble as synctest requires.
func underSynctest(t *testing.T, f func(t *testing.T, svc *application.Service, store *Postgres, hooks *notices)) {
	t.Helper()
	url := cluster.URL(t)
	synctest.Test(t, func(t *testing.T) {
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		store := NewPostgres(pool, nil)
		hooks := &notices{}
		svc, err := application.NewService(application.Deps{Store: store, Notifier: hooks, Hasher: password.NewHasher(cheapHash, 4, time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		f(t, svc, store, hooks)
	})
}

func loginAs(t *testing.T, svc *application.Service, username string) (token string, accountID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	acc, err := svc.Register(ctx, application.RegisterInput{Username: username, Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	token, err = svc.Login(ctx, application.LoginInput{Username: username, Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	return token, uuid.UUID(acc.ID)
}

func TestRotationGraceAndReuse(t *testing.T) {
	ctx := context.Background()

	t.Run("the retired token works once inside the grace; a second use is reuse and ends the session", func(t *testing.T) {
		underSynctest(t, func(t *testing.T, svc *application.Service, _ *Postgres, hooks *notices) {
			token, _ := loginAs(t, svc, "alice")
			p, err := svc.Validate(ctx, token)
			if err != nil {
				t.Fatal(err)
			}
			rotated, err := svc.Rotate(ctx, p.SessionID)
			if err != nil || rotated == token || rotated == "" {
				t.Fatalf("rotate: %q, %v", rotated, err)
			}
			time.Sleep(30 * time.Second)
			if q, err := svc.Validate(ctx, token); err != nil || q.SessionID != p.SessionID {
				t.Fatalf("old token inside the grace: %+v, %v", q, err)
			}
			if _, err := svc.Validate(ctx, rotated); err != nil {
				t.Fatalf("the current token after the old one was used once: %v", err)
			}
			if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("old token a second time: %v", err)
			}
			if _, err := svc.Validate(ctx, rotated); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("the current token after a reuse: %v", err)
			}
			if len(hooks.sessions) != 1 || hooks.sessions[0] != p.SessionID || hooks.reasons[0] != "token reuse" {
				t.Fatalf("hooks %v %v", hooks.sessions, hooks.reasons)
			}
			if _, err := svc.Rotate(ctx, p.SessionID); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("rotating a revoked session: %v", err)
			}
			if err := svc.Check(ctx, p.SessionID); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("check of a revoked session: %v", err)
			}
		})
	})

	t.Run("the retired token used after the grace is reuse too", func(t *testing.T) {
		underSynctest(t, func(t *testing.T, svc *application.Service, _ *Postgres, hooks *notices) {
			token, _ := loginAs(t, svc, "bob")
			p, err := svc.Validate(ctx, token)
			if err != nil {
				t.Fatal(err)
			}
			current, err := svc.Rotate(ctx, p.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(61 * time.Second)
			if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("retired token after the grace: %v", err)
			}
			if _, err := svc.Validate(ctx, current); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("the current token after a reuse: %v", err)
			}
			if len(hooks.sessions) != 1 || hooks.sessions[0] != p.SessionID || hooks.reasons[0] != "token reuse" {
				t.Fatalf("hooks %v %v", hooks.sessions, hooks.reasons)
			}
		})
	})

	t.Run("a second rotation retires the current token, not the one before", func(t *testing.T) {
		underSynctest(t, func(t *testing.T, svc *application.Service, _ *Postgres, _ *notices) {
			token, _ := loginAs(t, svc, "carol")
			p, _ := svc.Validate(ctx, token)
			second, _ := svc.Rotate(ctx, p.SessionID)
			third, _ := svc.Rotate(ctx, p.SessionID)
			if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
				t.Fatalf("the first token after two rotations: %v", err)
			}
			if _, err := svc.Validate(ctx, third); err != nil {
				t.Fatalf("an unknown token does not end the session: %v", err)
			}
			if _, err := svc.Validate(ctx, second); err != nil {
				t.Fatalf("the retired token inside the grace: %v", err)
			}
		})
	})
}

func TestSlidingExpiry(t *testing.T) {
	underSynctest(t, func(t *testing.T, svc *application.Service, store *Postgres, _ *notices) {
		ctx := context.Background()
		start := time.Now()
		at := func(d time.Duration) { time.Sleep(time.Until(start.Add(d))) }
		token, _ := loginAs(t, svc, "alice")
		p, err := svc.Validate(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		expiresAt := func() time.Time {
			t.Helper()
			s, err := store.FindSession(ctx, kernel.SessionID(p.SessionID))
			if err != nil {
				t.Fatal(err)
			}
			return s.ExpiresAt
		}
		day := 24 * time.Hour
		if got := expiresAt(); !got.Equal(start.Add(7 * day)) {
			t.Fatalf("after login expires at %v", got)
		}
		// After 5 minutes nothing moves; after 10 the idle week restarts.
		at(5 * time.Minute)
		if _, err := svc.Validate(ctx, token); err != nil {
			t.Fatal(err)
		}
		if got := expiresAt(); !got.Equal(start.Add(7 * day)) {
			t.Fatalf("extended after 5 minutes: %v", got)
		}
		at(10 * time.Minute)
		if _, err := svc.Validate(ctx, token); err != nil {
			t.Fatal(err)
		}
		if got := expiresAt(); !got.Equal(start.Add(10*time.Minute + 7*day)) {
			t.Fatalf("not extended after 10 minutes: %v", got)
		}
		// Used every six days, the session stays alive, until the 30-day cap.
		for _, d := range []time.Duration{6 * day, 12 * day, 18 * day, 24 * day} {
			at(d)
			if _, err := svc.Validate(ctx, token); err != nil {
				t.Fatalf("at %v: %v", d, err)
			}
		}
		if got := expiresAt(); !got.Equal(start.Add(30 * day)) {
			t.Fatalf("not capped at 30 days: %v", got)
		}
		at(29*day + 23*time.Hour)
		if _, err := svc.Validate(ctx, token); err != nil {
			t.Fatalf("just before the cap: %v", err)
		}
		at(30 * day)
		if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("at 30 days: %v", err)
		}
		if err := svc.Check(ctx, p.SessionID); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("check at 30 days: %v", err)
		}
		token2, _ := loginAs(t, svc, "bob")
		time.Sleep(7 * day)
		if _, err := svc.Validate(ctx, token2); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("idle for a week: %v", err)
		}
	})
}

func TestLogOutEverywhere(t *testing.T) {
	underSynctest(t, func(t *testing.T, svc *application.Service, _ *Postgres, hooks *notices) {
		ctx := context.Background()
		token, accountID := loginAs(t, svc, "alice")
		p, err := svc.Validate(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Check(ctx, p.SessionID); err != nil {
			t.Fatal(err)
		}
		if err := svc.RevokeAll(ctx, accountID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("after logging out everywhere: %v", err)
		}
		if err := svc.Check(ctx, p.SessionID); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("check after logging out everywhere: %v", err)
		}
		// The login fired the account hook once; logging out everywhere fired it again.
		if len(hooks.accounts) != 2 || hooks.accounts[1] != accountID || hooks.accountReasons[0] != application.ReasonNewLogin || hooks.accountReasons[1] != "logged out" {
			t.Fatalf("account hook %v %v", hooks.accounts, hooks.accountReasons)
		}
		token2, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		p2, _ := svc.Validate(ctx, token2)
		if err := svc.Revoke(ctx, p2.SessionID); err != nil {
			t.Fatal(err)
		}
		if len(hooks.sessions) != 1 || hooks.sessions[0] != p2.SessionID || hooks.reasons[0] != "logged out" {
			t.Fatalf("session hook %v %v", hooks.sessions, hooks.reasons)
		}
	})
}
