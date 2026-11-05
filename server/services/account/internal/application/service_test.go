package application_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"finga/pkg/auth"
	"finga/pkg/auth/password"
	"finga/pkg/uuidv7"
	"finga/services/account/internal/application"
	"finga/services/account/internal/model"
)

var cheapHash = password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}

// idleTTL is the session policy's idle lifetime (step 16); the tests that move time run under synctest.
const idleTTL = 7 * 24 * time.Hour

// unheard is a notifier for the tests that do not look at ended sessions.
type unheard struct{}

func (unheard) SessionRevoked(context.Context, uuid.UUID, string) {}
func (unheard) AccountRevoked(context.Context, uuid.UUID, string) {}

func newService(store application.Store) *application.Service {
	return application.NewService(store, unheard{}, application.Options{Hasher: password.NewHasher(cheapHash, 4, time.Second)})
}

func register(t *testing.T, svc *application.Service, username, pw string) model.Account {
	t.Helper()
	acc, err := svc.Register(context.Background(), application.RegisterInput{Username: username, Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func TestRegister(t *testing.T) {
	ctx := context.Background()
	t.Run("valid", func(t *testing.T) {
		store := newFakeStore()
		svc := newService(store)
		before := time.Now()
		acc := register(t, svc, "Alice_01", "correct horse")
		if acc.Username != "Alice_01" || acc.ID.IsZero() || acc.CreatedAt.Before(before) || acc.CreatedAt.After(time.Now()) || acc.Settings != model.DefaultSettings() {
			t.Fatalf("account = %+v", acc)
		}
		stored := store.accounts[acc.ID]
		if !strings.HasPrefix(stored.PasswordHash, "$argon2id$v=19$") {
			t.Fatalf("stored hash is not argon2id PHC: %q", stored.PasswordHash)
		}
		if strings.Contains(stored.PasswordHash, "correct horse") {
			t.Fatal("the password reached the repository")
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		tests := []struct {
			name     string
			username string
			password string
			want     error
		}{
			{"username too short", "ab", "longenough", model.ErrBadUsername},
			{"username too long", strings.Repeat("a", 17), "longenough", model.ErrBadUsername},
			{"username with a space", "al ice", "longenough", model.ErrBadUsername},
			{"username with a dash", "al-ice", "longenough", model.ErrBadUsername},
			{"username with unicode letters", "alicé", "longenough", model.ErrBadUsername},
			{"empty username", "", "longenough", model.ErrBadUsername},
			{"password too short", "alice", "1234567", model.ErrBadPassword},
			{"password too long", "alice", strings.Repeat("p", 129), model.ErrBadPassword},
			{"empty password", "alice", "", model.ErrBadPassword},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				store := newFakeStore()
				svc := newService(store)
				_, err := svc.Register(ctx, application.RegisterInput{Username: tt.username, Password: tt.password})
				if !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				if store.txCount != 0 {
					t.Fatal("an invalid input reached the store")
				}
			})
		}
	})

	t.Run("boundary lengths are accepted", func(t *testing.T) {
		svc := newService(newFakeStore())
		register(t, svc, "abc", "12345678")
		register(t, svc, strings.Repeat("Z", 16), strings.Repeat("p", 128))
	})

	t.Run("a duplicate becomes conflict", func(t *testing.T) {
		svc := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		_, err := svc.Register(ctx, application.RegisterInput{Username: "ALICE", Password: "correct horse"})
		if !errors.Is(err, model.ErrUsernameTaken) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestVerify(t *testing.T) {
	ctx := context.Background()
	svc := newService(newFakeStore())
	acc := register(t, svc, "alice", "correct horse")
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{"correct password", "alice", "correct horse", false},
		{"username in another case", "ALICE", "correct horse", false},
		{"wrong password", "alice", "correct horsE", true},
		{"unknown user", "bob", "correct horse", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := svc.Verify(ctx, tt.username, tt.password)
			if tt.wantErr {
				if !errors.Is(err, model.ErrUnauthenticated) {
					t.Fatalf("err = %v, want ErrUnauthenticated", err)
				}
				return
			}
			if err != nil || id != uuid.UUID(acc.ID) {
				t.Fatalf("id = %s, err = %v", id, err)
			}
		})
	}
}

func TestLoginIssuesATokenAndStoresOnlyItsHash(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	svc := newService(store)
	acc := register(t, svc, "alice", "correct horse")
	before := time.Now()
	token, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 {
		t.Fatalf("token %q looks short for 32 random bytes", token)
	}
	if len(store.insertedTokenHashes) != 1 {
		t.Fatalf("%d sessions inserted", len(store.insertedTokenHashes))
	}
	want := sha256.Sum256([]byte(token))
	if string(store.insertedTokenHashes[0]) != string(want[:]) {
		t.Fatal("the stored hash is not SHA-256 of the token")
	}
	for _, se := range store.sessions {
		if strings.Contains(string(se.TokenHash), token) {
			t.Fatal("the token itself reached the repository")
		}
		if se.ExpiresAt.Before(before.Add(idleTTL)) || se.ExpiresAt.After(after.Add(idleTTL)) || se.AccountID != acc.ID {
			t.Fatalf("session = %+v", se)
		}
	}
	p, err := svc.Validate(ctx, token)
	if err != nil || p.AccountID != uuid.UUID(acc.ID) || p.SessionID == uuid.Nil {
		t.Fatalf("validate = %+v, %v", p, err)
	}
	t.Run("a second login revokes the first session", func(t *testing.T) {
		token2, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		if token2 == token {
			t.Fatal("same token twice")
		}
		if _, err := svc.Validate(ctx, token); !errors.Is(err, model.ErrUnauthenticated) {
			t.Fatalf("first token still validates: %v", err)
		}
		if store.activeSessions(acc.ID) != 1 {
			t.Fatalf("%d active sessions", store.activeSessions(acc.ID))
		}
	})
}

func TestValidateFailsTheSameWayForEveryBadToken(t *testing.T) {
	ctx := context.Background()
	login := func(t *testing.T, svc *application.Service) string {
		t.Helper()
		register(t, svc, "alice", "correct horse")
		token, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	tests := []struct {
		name  string
		token func(t *testing.T, svc *application.Service) string
	}{
		{"unknown token", func(*testing.T, *application.Service) string { return "not-a-token" }},
		{"empty token", func(*testing.T, *application.Service) string { return "" }},
		{"expired token", func(t *testing.T, svc *application.Service) string {
			tok := login(t, svc)
			time.Sleep(idleTTL) // exactly the lifetime: no longer valid
			return tok
		}},
		{"revoked token", func(t *testing.T, svc *application.Service) string {
			tok := login(t, svc)
			p, err := svc.Validate(ctx, tok)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.Revoke(ctx, p.SessionID); err != nil {
				t.Fatal(err)
			}
			return tok
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				svc := newService(newFakeStore())
				_, err := svc.Validate(ctx, tt.token(t, svc))
				if !errors.Is(err, model.ErrUnauthenticated) {
					t.Fatalf("err = %v, want ErrUnauthenticated", err)
				}
			})
		})
	}
	t.Run("a token just before expiry still validates", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			svc := newService(newFakeStore())
			tok := login(t, svc)
			time.Sleep(idleTTL - time.Second)
			if _, err := svc.Validate(ctx, tok); err != nil {
				t.Fatal(err)
			}
		})
	})
}

func TestLoginThrottling(t *testing.T) {
	ctx := context.Background()
	t.Run("too many attempts for one username give rate_limited, which resets after the window", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			svc := newService(newFakeStore())
			register(t, svc, "alice", "correct horse")
			for i := range 5 {
				if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "wrong"}); !errors.Is(err, model.ErrUnauthenticated) {
					t.Fatalf("attempt %d: %v", i, err)
				}
			}
			_, err := svc.Login(ctx, application.LoginInput{Username: "ALICE", Password: "correct horse"})
			if !errors.Is(err, model.ErrRateLimited) {
				t.Fatalf("sixth attempt: %v, want rate_limited even with the right password", err)
			}
			time.Sleep(time.Minute)
			if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"}); err != nil {
				t.Fatalf("after the window: %v", err)
			}
		})
	})
	t.Run("one username's throttle leaves the others alone: per-IP limits are the edge's (nginx)", func(t *testing.T) {
		svc := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		for i := range 5 {
			_, _ = svc.Login(ctx, application.LoginInput{Username: fmt.Sprintf("user%d", i), Password: "wrong"})
		}
		if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"}); err != nil {
			t.Fatalf("another username: %v", err)
		}
	})
	t.Run("Verify itself is not throttled: the handler's job", func(t *testing.T) {
		svc := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		for range 10 {
			if _, err := svc.Verify(ctx, "alice", "correct horse"); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	svc := newService(newFakeStore())
	acc := register(t, svc, "alice", "correct horse")
	tests := []struct {
		name    string
		preset  model.Preset
		wantErr error
	}{
		{"classic", model.PresetClassic, nil},
		{"wasd", model.PresetWASD, nil},
		{"unknown preset is rejected", "vim", model.ErrBadPreset},
		{"empty preset is rejected", "", model.ErrBadPreset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.UpdateSettings(ctx, acc.ID, model.Settings{ControlPreset: tt.preset})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			got, err := svc.Settings(ctx, acc.ID)
			if err != nil || got.ControlPreset != tt.preset {
				t.Fatalf("settings = %+v, %v", got, err)
			}
		})
	}
}

func TestServiceImplementsPkgAuth(t *testing.T) {
	var _ auth.Authenticator = (*application.Service)(nil)
	var _ auth.Sessions = (*application.Service)(nil)
	t.Run("Issue for an unknown account fails", func(t *testing.T) {
		svc := newService(newFakeStore())
		if _, err := svc.Issue(context.Background(), uuidv7.New()); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a store failure surfaces", func(t *testing.T) {
		store := newFakeStore()
		svc := newService(store)
		store.failInTx = errors.New("db down")
		if _, err := svc.Register(context.Background(), application.RegisterInput{Username: "alice", Password: "correct horse"}); err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("err = %v", err)
		}
	})
}

// A burst of sign-ups past the hash slots answers busy once the wait runs out, instead of taking
// 64 MiB a hash.
func TestSignUpsPastTheHashSlotsAreBusy(t *testing.T) {
	svc := application.NewService(newFakeStore(), unheard{}, application.Options{Hasher: password.NewHasher(password.Default, 1, time.Millisecond)})
	var wg sync.WaitGroup
	var ok, busy atomic.Int32
	for k := range 16 {
		wg.Go(func() {
			_, err := svc.Register(context.Background(), application.RegisterInput{Username: fmt.Sprintf("player_%d", k), Password: "correct horse"})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, model.ErrBusy):
				busy.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok.Load() == 0 || busy.Load() == 0 {
		t.Fatalf("%d signed up, %d busy; want some of each with one slot", ok.Load(), busy.Load())
	}
}
