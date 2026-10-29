package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/pkg/auth/password"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
)

// The rows the step page wants against real Postgres: the partial unique
// index is the backstop for one active session.

func newPGService(t *testing.T) (*application.Service, *Postgres) {
	t.Helper()
	store := NewPostgres(cluster.DB(t))
	svc := application.NewService(store, application.Options{
		IdleTTL: time.Hour,
		Hash:    password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32},
	})
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = svc.Issue(ctx, uuid.UUID(acc.ID)) // Login would be throttled; the race is in Issue
		}()
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
