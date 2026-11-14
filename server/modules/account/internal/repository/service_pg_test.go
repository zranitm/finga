package repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finga/modules/account/internal/application"
	"finga/modules/account/internal/model"
	"finga/modules/account/internal/password"
)

// The partial unique index is the backstop for one active session.

func newPGService(t *testing.T) (*application.Service, *Postgres) {
	t.Helper()
	store := NewPostgres(cluster.DB(t), nil)
	svc, err := application.NewService(application.Deps{Store: store, Notifier: &notices{}, Hasher: password.NewHasher(cheapHash, 4, time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func TestLoggingInTwiceStopsTheFirstSession(t *testing.T) {
	ctx := context.Background()
	svc, _ := newPGService(t)
	if _, err := svc.Register(ctx, application.RegisterInput{Username: "alice", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Validate(ctx, first); !errors.Is(err, model.ErrUnauthenticated) {
		t.Fatalf("first session still validates: %v", err)
	}
	if _, err := svc.Validate(ctx, second); err != nil {
		t.Fatalf("second session: %v", err)
	}
}

func TestConcurrentLoginsLeaveExactlyOneActiveSession(t *testing.T) {
	ctx := context.Background()
	svc, store := newPGService(t)
	acc, err := svc.Register(ctx, application.RegisterInput{Username: "alice", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	tokens := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			tokens[i], errs[i] = svc.Issue(ctx, acc.ID) // Login would be throttled; the race is in Issue
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
	}
	var active int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE account_id = $1 AND revoked_at IS NULL`, acc.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("%d active sessions, want 1", active)
	}
	valid := 0
	for _, tok := range tokens {
		if _, err := svc.Validate(ctx, tok); err == nil {
			valid++
		}
	}
	if valid != 1 {
		t.Fatalf("%d tokens validate, want 1", valid)
	}
}

// The retired token works once within the grace, however many requests race with it; the rest are reuse.
func TestTheRetiredTokenWorksOnceUnderARace(t *testing.T) {
	ctx := context.Background()
	svc, store := newPGService(t)
	acc, err := svc.Register(ctx, application.RegisterInput{Username: "alice", Password: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := svc.Issue(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Validate(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Rotate(ctx, p.SessionID); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	var ok atomic.Int32
	for range n {
		wg.Go(func() {
			if _, err := svc.Validate(ctx, old); err == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if got := ok.Load(); got != 1 {
		t.Fatalf("the retired token validated %d times, want once", got)
	}
	var revoked bool
	if err := store.pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE id = $1`, p.SessionID).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("the reuse left the session active")
	}
}
