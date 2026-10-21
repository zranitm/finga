package account_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"finga/internal/application/account"
	"finga/pkg/auth"
	"finga/pkg/auth/password"
	"finga/pkg/errs"
)

var cheapHash = password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(store account.Store) (*account.Service, *clock) {
	c := &clock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	return account.NewService(store, account.Options{SessionTTL: time.Hour, Now: c.now, Hash: cheapHash}), c
}

func register(t *testing.T, svc *account.Service, username, pw string) account.Account {
	t.Helper()
	acc, err := svc.Register(context.Background(), account.RegisterInput{Username: username, Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	return acc
}

func TestRegister(t *testing.T) {
	ctx := context.Background()
	t.Run("valid", func(t *testing.T) {
		store := newFakeStore()
		svc, c := newService(store)
		acc := register(t, svc, "Alice_01", "correct horse")
		if acc.Username != "Alice_01" || acc.ID.IsZero() || !acc.CreatedAt.Equal(c.t) || acc.Settings != account.DefaultSettings() {
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
			{"username too short", "ab", "longenough", account.ErrBadUsername},
			{"username too long", strings.Repeat("a", 17), "longenough", account.ErrBadUsername},
			{"username with a space", "al ice", "longenough", account.ErrBadUsername},
			{"username with a dash", "al-ice", "longenough", account.ErrBadUsername},
			{"username with unicode letters", "alicé", "longenough", account.ErrBadUsername},
			{"empty username", "", "longenough", account.ErrBadUsername},
			{"password too short", "alice", "1234567", account.ErrBadPassword},
			{"password too long", "alice", strings.Repeat("p", 129), account.ErrBadPassword},
			{"empty password", "alice", "", account.ErrBadPassword},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				store := newFakeStore()
				svc, _ := newService(store)
				_, err := svc.Register(ctx, account.RegisterInput{Username: tt.username, Password: tt.password})
				if !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				if errs.CodeOf(err) != errs.InvalidArgument {
					t.Fatalf("code = %s", errs.CodeOf(err))
				}
				if store.txCount != 0 {
					t.Fatal("an invalid input reached the store")
				}
			})
		}
	})

	t.Run("boundary lengths are accepted", func(t *testing.T) {
		svc, _ := newService(newFakeStore())
		register(t, svc, "abc", "12345678")
		register(t, svc, strings.Repeat("Z", 16), strings.Repeat("p", 128))
	})

	t.Run("a duplicate becomes conflict", func(t *testing.T) {
		svc, _ := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		_, err := svc.Register(ctx, account.RegisterInput{Username: "ALICE", Password: "correct horse"})
		if !errors.Is(err, account.ErrUsernameTaken) || errs.CodeOf(err) != errs.Conflict {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestVerify(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(newFakeStore())
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
				if !errors.Is(err, account.ErrUnauthenticated) {
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
	svc, c := newService(store)
	acc := register(t, svc, "alice", "correct horse")
	token, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse", IP: "10.0.0.1"})
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
		if !se.ExpiresAt.Equal(c.t.Add(time.Hour)) || se.AccountID != acc.ID {
			t.Fatalf("session = %+v", se)
		}
	}
	p, err := svc.Validate(ctx, token)
	if err != nil || p.AccountID != uuid.UUID(acc.ID) || p.SessionID == uuid.Nil {
		t.Fatalf("validate = %+v, %v", p, err)
	}
	t.Run("a second login revokes the first session", func(t *testing.T) {
		token2, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		if token2 == token {
			t.Fatal("same token twice")
		}
		if _, err := svc.Validate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
			t.Fatalf("first token still validates: %v", err)
		}
		if store.activeSessions(acc.ID) != 1 {
			t.Fatalf("%d active sessions", store.activeSessions(acc.ID))
		}
	})
}

func TestValidateFailsTheSameWayForEveryBadToken(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	svc, c := newService(store)
	register(t, svc, "alice", "correct horse")
	login := func(t *testing.T) string {
		t.Helper()
		token, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"unknown token", func(*testing.T) string { return "not-a-token" }},
		{"empty token", func(*testing.T) string { return "" }},
		{"expired token", func(t *testing.T) string {
			tok := login(t)
			c.t = c.t.Add(time.Hour) // exactly the lifetime: no longer valid
			return tok
		}},
		{"revoked token", func(t *testing.T) string {
			tok := login(t)
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
			_, err := svc.Validate(ctx, tt.token(t))
			if !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("err = %v, want ErrUnauthenticated", err)
			}
			if errs.CodeOf(err) != errs.Unauthenticated {
				t.Fatalf("code = %s", errs.CodeOf(err))
			}
			if msg := errs.MessageOf(err); strings.Contains(msg, "expired") || strings.Contains(msg, "revoked") || strings.Contains(msg, "unknown") {
				t.Fatalf("message reveals the reason: %q", msg)
			}
		})
	}
	t.Run("a token just before expiry still validates", func(t *testing.T) {
		tok := login(t)
		c.t = c.t.Add(time.Hour - time.Second)
		if _, err := svc.Validate(ctx, tok); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoginThrottling(t *testing.T) {
	ctx := context.Background()
	t.Run("too many attempts for one username give rate_limited, which resets after the window", func(t *testing.T) {
		svc, c := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		for i := range 5 {
			if _, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "wrong", IP: fmt.Sprintf("10.0.0.%d", i)}); !errors.Is(err, account.ErrUnauthenticated) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
		_, err := svc.Login(ctx, account.LoginInput{Username: "ALICE", Password: "correct horse", IP: "10.0.0.9"})
		if !errors.Is(err, account.ErrRateLimited) || errs.CodeOf(err) != errs.RateLimited {
			t.Fatalf("sixth attempt: %v, want rate_limited even with the right password", err)
		}
		c.t = c.t.Add(time.Minute)
		if _, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse", IP: "10.0.0.9"}); err != nil {
			t.Fatalf("after the window: %v", err)
		}
	})
	t.Run("too many attempts from one IP give rate_limited across usernames", func(t *testing.T) {
		svc, _ := newService(newFakeStore())
		register(t, svc, "alice", "correct horse")
		for i := range 5 {
			_, _ = svc.Login(ctx, account.LoginInput{Username: fmt.Sprintf("user%d", i), Password: "wrong", IP: "10.0.0.1"})
		}
		_, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse", IP: "10.0.0.1"})
		if !errors.Is(err, account.ErrRateLimited) {
			t.Fatalf("err = %v", err)
		}
		if _, err := svc.Login(ctx, account.LoginInput{Username: "alice", Password: "correct horse", IP: "10.0.0.2"}); err != nil {
			t.Fatalf("another IP: %v", err)
		}
	})
	t.Run("Verify itself is not throttled: the handler's job", func(t *testing.T) {
		svc, _ := newService(newFakeStore())
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
	svc, _ := newService(newFakeStore())
	acc := register(t, svc, "alice", "correct horse")
	tests := []struct {
		name    string
		preset  account.Preset
		wantErr error
	}{
		{"classic", account.PresetClassic, nil},
		{"wasd", account.PresetWASD, nil},
		{"unknown preset is rejected", "vim", account.ErrBadPreset},
		{"empty preset is rejected", "", account.ErrBadPreset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.UpdateSettings(ctx, acc.ID, account.Settings{ControlPreset: tt.preset})
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
	var _ auth.Authenticator = (*account.Service)(nil)
	var _ auth.Sessions = (*account.Service)(nil)
	t.Run("Issue for an unknown account fails", func(t *testing.T) {
		svc, _ := newService(newFakeStore())
		if _, err := svc.Issue(context.Background(), uuid.New()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a store failure surfaces", func(t *testing.T) {
		store := newFakeStore()
		svc, _ := newService(store)
		store.failInTx = errors.New("db down")
		if _, err := svc.Register(context.Background(), account.RegisterInput{Username: "alice", Password: "correct horse"}); err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("err = %v", err)
		}
	})
}
