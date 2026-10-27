package account

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/internal/model"
	"finga/pkg/auth/password"
)

// Step 16 rows against real Postgres with an injected clock: rotation with
// its grace, reuse detection, the sliding expiry, logging out everywhere.

type hookRecorder struct {
	mu             sync.Mutex
	sessions       []uuid.UUID
	reasons        []string
	accounts       []uuid.UUID
	accountReasons []string
}

func (h *hookRecorder) sessionRevoked(_ context.Context, id uuid.UUID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, id)
	h.reasons = append(h.reasons, reason)
}

func (h *hookRecorder) accountRevoked(_ context.Context, id uuid.UUID, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accounts = append(h.accounts, id)
	h.accountReasons = append(h.accountReasons, reason)
}

func newClockedPGService(t *testing.T) (*Service, *Postgres, *time.Time, *hookRecorder) {
	t.Helper()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	store := NewPostgres(cluster.DB(t))
	hooks := &hookRecorder{}
	svc := NewService(store, Options{
		Now:              func() time.Time { return now },
		Hash:             password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32},
		OnSessionRevoked: hooks.sessionRevoked,
		OnAccountRevoked: hooks.accountRevoked,
	})
	return svc, store, &now, hooks
}

func loginAs(t *testing.T, svc *Service, username string) (token string, accountID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	acc, err := svc.Register(ctx, RegisterInput{Username: username, Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	token, err = svc.Login(ctx, LoginInput{Username: username, Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	return token, uuid.UUID(acc.ID)
}

func TestRotationGraceAndReuse(t *testing.T) {
	ctx := context.Background()
	svc, _, now, hooks := newClockedPGService(t)

	t.Run("the retired token works once inside the grace; a second use is reuse and ends the session", func(t *testing.T) {
		token, _ := loginAs(t, svc, "alice")
		p, err := svc.Validate(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		rotated, err := svc.Rotate(ctx, p.SessionID)
		if err != nil || rotated == token || rotated == "" {
			t.Fatalf("rotate: %q, %v", rotated, err)
		}
		*now = now.Add(30 * time.Second)
		if q, err := svc.Validate(ctx, token); err != nil || q.SessionID != p.SessionID {
			t.Fatalf("old token inside the grace: %+v, %v", q, err)
		}
		if _, err := svc.Validate(ctx, rotated); err != nil {
			t.Fatalf("the current token after the old one was used once: %v", err)
		}
		if _, err := svc.Validate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("old token a second time: %v", err)
		}
		if _, err := svc.Validate(ctx, rotated); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("the current token after a reuse: %v", err)
		}
		if len(hooks.sessions) != 1 || hooks.sessions[0] != p.SessionID || hooks.reasons[0] != "token reuse" {
			t.Fatalf("hooks %v %v", hooks.sessions, hooks.reasons)
		}
		if _, err := svc.Rotate(ctx, p.SessionID); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("rotating a revoked session: %v", err)
		}
		if err := svc.Check(ctx, p.SessionID); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("check of a revoked session: %v", err)
		}
	})

	t.Run("the retired token used after the grace is reuse too", func(t *testing.T) {
		token, _ := loginAs(t, svc, "bob")
		p, err := svc.Validate(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
		current, err := svc.Rotate(ctx, p.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		*now = now.Add(61 * time.Second)
		if _, err := svc.Validate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("retired token after the grace: %v", err)
		}
		if _, err := svc.Validate(ctx, current); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("the current token after a reuse: %v", err)
		}
		if len(hooks.sessions) != 2 || hooks.sessions[1] != p.SessionID || hooks.reasons[1] != "token reuse" {
			t.Fatalf("hooks %v %v", hooks.sessions, hooks.reasons)
		}
	})

	t.Run("a second rotation retires the current token, not the one before", func(t *testing.T) {
		token, _ := loginAs(t, svc, "carol")
		p, _ := svc.Validate(ctx, token)
		second, _ := svc.Rotate(ctx, p.SessionID)
		third, _ := svc.Rotate(ctx, p.SessionID)
		if _, err := svc.Validate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("the first token after two rotations: %v", err)
		}
		if _, err := svc.Validate(ctx, third); err != nil {
			t.Fatalf("an unknown token does not end the session: %v", err)
		}
		if _, err := svc.Validate(ctx, second); err != nil {
			t.Fatalf("the retired token inside the grace: %v", err)
		}
	})
}

func TestSlidingExpiry(t *testing.T) {
	ctx := context.Background()
	svc, store, now, _ := newClockedPGService(t)
	start := *now
	token, _ := loginAs(t, svc, "alice")
	p, err := svc.Validate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := func() time.Time {
		t.Helper()
		s, err := store.FindSession(ctx, model.SessionID(p.SessionID))
		if err != nil {
			t.Fatal(err)
		}
		return s.ExpiresAt
	}
	day := 24 * time.Hour
	if got := expiresAt(); !got.Equal(start.Add(7 * day)) {
		t.Fatalf("after login expires at %v", got)
	}
	// Used again after 5 minutes: nothing moves; after 10: the idle week
	// restarts from then.
	*now = start.Add(5 * time.Minute)
	if _, err := svc.Validate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if got := expiresAt(); !got.Equal(start.Add(7 * day)) {
		t.Fatalf("extended after 5 minutes: %v", got)
	}
	*now = start.Add(10 * time.Minute)
	if _, err := svc.Validate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if got := expiresAt(); !got.Equal(start.Add(10*time.Minute + 7*day)) {
		t.Fatalf("not extended after 10 minutes: %v", got)
	}
	// Used every six days, the session stays alive, until the 30-day cap.
	for _, d := range []time.Duration{6 * day, 12 * day, 18 * day, 24 * day} {
		*now = start.Add(d)
		if _, err := svc.Validate(ctx, token); err != nil {
			t.Fatalf("at %v: %v", d, err)
		}
	}
	if got := expiresAt(); !got.Equal(start.Add(30 * day)) {
		t.Fatalf("not capped at 30 days: %v", got)
	}
	*now = start.Add(29*day + 23*time.Hour)
	if _, err := svc.Validate(ctx, token); err != nil {
		t.Fatalf("just before the cap: %v", err)
	}
	*now = start.Add(30 * day)
	if _, err := svc.Validate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("at 30 days: %v", err)
	}
	if err := svc.Check(ctx, p.SessionID); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("check at 30 days: %v", err)
	}
	// A session left idle for a week is gone.
	token2, _ := loginAs(t, svc, "bob")
	*now = now.Add(7 * day)
	if _, err := svc.Validate(ctx, token2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle for a week: %v", err)
	}
}

func TestLogOutEverywhere(t *testing.T) {
	ctx := context.Background()
	svc, _, _, hooks := newClockedPGService(t)
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
	if _, err := svc.Validate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after logging out everywhere: %v", err)
	}
	if err := svc.Check(ctx, p.SessionID); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("check after logging out everywhere: %v", err)
	}
	// The login itself fired the account hook once (a new login ends older
	// sessions); logging out everywhere fired it again.
	if len(hooks.accounts) != 2 || hooks.accounts[1] != accountID || hooks.accountReasons[0] != ReasonNewLogin || hooks.accountReasons[1] != "logged out" {
		t.Fatalf("account hook %v %v", hooks.accounts, hooks.accountReasons)
	}
	// Logging out one session fires the session hook.
	token2, err := svc.Login(ctx, LoginInput{Username: "alice", Password: "correct horse"})
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
}

func TestUnknownUsernameVerifiesADummyHash(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newClockedPGService(t)
	if svc.dummy == "" {
		t.Fatal("no dummy hash")
	}
	if _, err := svc.Verify(ctx, "nobody", "whatever"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown user: %v", err)
	}
}
