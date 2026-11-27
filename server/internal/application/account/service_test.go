package account_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	application "finga/internal/application/account"
	"finga/internal/application/account/password"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

var cheapHash = password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}

// idleTTL is the session policy's idle lifetime; the tests that move time run under synctest.
const idleTTL = 7 * 24 * time.Hour

// unheard is a notifier for the tests that do not look at ended sessions.
type unheard struct{}

func (unheard) SessionRevoked(context.Context, kernel.SessionID, string) {}
func (unheard) AccountRevoked(context.Context, kernel.AccountID, string) {}

func newService(t *testing.T, store application.Store) *application.Service {
	t.Helper()
	return build(t, application.ServiceDeps{Store: store, Notifier: unheard{}, Hasher: password.NewHasher(cheapHash, 4, time.Second)})
}

func build(t *testing.T, deps application.ServiceDeps) *application.Service {
	t.Helper()
	svc, err := application.NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	return svc
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
		svc := newService(t, store)
		acc := register(t, svc, "Alice_01", "correct horse")
		stored := store.accounts[acc.ID]
		// created_at is the store's, not a clock the service reads.
		if acc.Username != "Alice_01" || acc.ID.IsZero() || acc.CreatedAt.IsZero() || !acc.CreatedAt.Equal(stored.Account.CreatedAt) || !reflect.DeepEqual(acc.Settings, model.Settings{}) {
			t.Fatalf("account = %+v", acc)
		}
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
				svc := newService(t, store)
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
		svc := newService(t, newFakeStore())
		register(t, svc, "abc", "12345678")
		register(t, svc, strings.Repeat("Z", 16), strings.Repeat("p", 128))
	})

	t.Run("a duplicate becomes conflict", func(t *testing.T) {
		svc := newService(t, newFakeStore())
		register(t, svc, "alice", "correct horse")
		_, err := svc.Register(ctx, application.RegisterInput{Username: "ALICE", Password: "correct horse"})
		if !errors.Is(err, model.ErrUsernameTaken) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestVerify(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, newFakeStore())
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
			if err != nil || id != acc.ID {
				t.Fatalf("id = %s, err = %v", id, err)
			}
		})
	}
}

// Once the cost is raised, an old hash checks faster than an unknown username's dummy, so a login upgrades it.
func TestALoginUpgradesAHashMadeAtAnotherCost(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	acc := register(t, newService(t, store), "alice", "correct horse")
	old := store.accounts[acc.ID].PasswordHash
	raised := cheapHash
	raised.Time = 2
	svc := build(t, application.ServiceDeps{Store: store, Notifier: unheard{}, Hasher: password.NewHasher(raised, 4, time.Second)})

	if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "wrong horse"}); !errors.Is(err, model.ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
	if store.accounts[acc.ID].PasswordHash != old {
		t.Fatal("a failed login replaced the hash")
	}
	if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	upgraded := store.accounts[acc.ID].PasswordHash
	if !strings.Contains(upgraded, "$m=8192,t=2,p=1$") {
		t.Fatalf("after a login the hash is %q, want it at the configured cost", upgraded)
	}
	if ok, err := password.Verify(upgraded, "correct horse"); err != nil || !ok {
		t.Fatalf("the upgraded hash does not verify: %v %v", ok, err)
	}
	if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"}); err != nil {
		t.Fatal(err)
	}
	if store.accounts[acc.ID].PasswordHash != upgraded {
		t.Fatal("a hash already at the configured cost was made again")
	}
}

func TestLoginIssuesATokenAndStoresOnlyItsHash(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	svc := newService(t, store)
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
	if err != nil || p.AccountID != acc.ID || p.SessionID.IsZero() {
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
				svc := newService(t, newFakeStore())
				_, err := svc.Validate(ctx, tt.token(t, svc))
				if !errors.Is(err, model.ErrUnauthenticated) {
					t.Fatalf("err = %v, want ErrUnauthenticated", err)
				}
			})
		})
	}
	t.Run("a token just before expiry still validates", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			svc := newService(t, newFakeStore())
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
			svc := newService(t, newFakeStore())
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
		svc := newService(t, newFakeStore())
		register(t, svc, "alice", "correct horse")
		for i := range 5 {
			_, _ = svc.Login(ctx, application.LoginInput{Username: fmt.Sprintf("user%d", i), Password: "wrong"})
		}
		if _, err := svc.Login(ctx, application.LoginInput{Username: "alice", Password: "correct horse"}); err != nil {
			t.Fatalf("another username: %v", err)
		}
	})
	t.Run("Verify itself is not throttled: the handler's job", func(t *testing.T) {
		svc := newService(t, newFakeStore())
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
	svc := newService(t, newFakeStore())
	acc := register(t, svc, "alice", "correct horse")
	// Any action the client names: the server checks the shape, and a left Shift on its own is a key.
	want := model.Settings{AutoAssignPoints: true, HideMinimap: true, Zoom: 0.375, Walk: true, Keys: map[string]string{"attack": "KeyV", "skill1": "ShiftLeft", "someLaterAction": "Digit9"}}
	if err := svc.UpdateSettings(ctx, acc.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Settings(ctx, acc.ID); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %+v, %v; want %+v", got, err, want)
	}
	many := map[string]string{}
	for i := range model.MaxKeyBindings + 1 {
		many[fmt.Sprintf("a%d", i)] = fmt.Sprintf("K%d", i)
	}
	for name, keys := range map[string]map[string]string{
		"one key for two actions":     {"attack": "KeyV", "skill1": "KeyV"},
		"Enter, the chat's":           {"attack": "Enter"},
		"the numpad's Enter":          {"attack": "NumpadEnter"},
		"Esc, the windows'":           {"close": "Escape"},
		"the Windows or Command key":  {"attack": "MetaLeft"},
		"the menu key":                {"attack": "ContextMenu"},
		"F5, the browser's":           {"attack": "F5"},
		"F11":                         {"attack": "F11"},
		"F12":                         {"attack": "F12"},
		"an empty key":                {"attack": ""},
		"a key that isn't a word":     {"attack": "Key V"},
		"a key past 32 letters":       {"attack": strings.Repeat("K", 33)},
		"an action that isn't a word": {"at-tack": "KeyV"},
		"more than the most kept":     many,
	} {
		if err := svc.UpdateSettings(ctx, acc.ID, model.Settings{Keys: keys}); !errors.Is(err, model.ErrBadKeyBindings) {
			t.Errorf("%s: %v, want ErrBadKeyBindings", name, err)
		}
	}
	for _, zoom := range []float64{-0.001, 1.001, -1, 2, math.NaN()} {
		if err := svc.UpdateSettings(ctx, acc.ID, model.Settings{Zoom: zoom}); !errors.Is(err, model.ErrBadZoom) {
			t.Errorf("zoom %v: %v, want ErrBadZoom", zoom, err)
		}
	}
	if got, _ := svc.Settings(ctx, acc.ID); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings %+v after refusals, want %+v kept", got, want)
	}
	exactly := map[string]string{}
	for i := range model.MaxKeyBindings {
		exactly[fmt.Sprintf("a%d", i)] = fmt.Sprintf("K%d", i)
	}
	if err := svc.UpdateSettings(ctx, acc.ID, model.Settings{Keys: exactly}); err != nil {
		t.Fatalf("the most kept: %v", err)
	}
	for _, zoom := range []float64{0, 1} {
		if err := svc.UpdateSettings(ctx, acc.ID, model.Settings{Zoom: zoom}); err != nil {
			t.Errorf("zoom %v, at an end: %v", zoom, err)
		}
	}
}

func TestServiceSurfacesItsFailures(t *testing.T) {
	t.Run("Issue for an unknown account fails", func(t *testing.T) {
		svc := newService(t, newFakeStore())
		if _, err := svc.Issue(context.Background(), kernel.NewAccountID()); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a store failure surfaces", func(t *testing.T) {
		store := newFakeStore()
		svc := newService(t, store)
		store.failInTx = errors.New("db down")
		if _, err := svc.Register(context.Background(), application.RegisterInput{Username: "alice", Password: "correct horse"}); err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("err = %v", err)
		}
	})
}

// A burst of sign-ups past the hash slots answers busy once the wait runs out, instead of taking 64 MiB a hash.
func TestSignUpsPastTheHashSlotsAreBusy(t *testing.T) {
	svc := build(t, application.ServiceDeps{Store: newFakeStore(), Notifier: unheard{}, Hasher: password.NewHasher(password.Default, 1, time.Millisecond)})
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
