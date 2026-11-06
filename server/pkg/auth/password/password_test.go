package password_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finga/pkg/auth/password"
)

// cheap keeps the tests fast; production uses password.Default.
var cheap = password.Params{Time: 1, Memory: 8 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashFormat(t *testing.T) {
	h, err := password.Hash("correct horse battery staple", cheap)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(h, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=8192,t=1,p=1" {
		t.Fatalf("not PHC argon2id: %q", h)
	}
	if parts[4] == "" || parts[5] == "" {
		t.Fatalf("empty salt or hash: %q", h)
	}
	again, err := password.Hash("correct horse battery staple", cheap)
	if err != nil {
		t.Fatal(err)
	}
	if again == h {
		t.Fatal("two hashes of one password are equal: no random salt")
	}
}

func TestVerify(t *testing.T) {
	h, err := password.Hash("hunter22", cheap)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		hash     string
		password string
		want     bool
		wantErr  bool
	}{
		{"correct password", h, "hunter22", true, false},
		{"wrong password", h, "hunter23", false, false},
		{"empty password", h, "", false, false},
		{"not a PHC string", "plain", "hunter22", false, true},
		{"wrong algorithm", strings.Replace(h, "argon2id", "argon2i", 1), "hunter22", false, true},
		{"tampered parameters", strings.Replace(h, "t=1", "t=2", 1), "hunter22", false, false},
		{"tampered hash", h[:len(h)-4] + "AAAA", "hunter22", false, false},
		{"corrupt salt", strings.Replace(h, "$"+strings.Split(h, "$")[4]+"$", "$!!$", 1), "hunter22", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := password.Verify(tt.hash, tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultParamsVerify(t *testing.T) {
	h, err := password.Hash("a passphrase", password.Default)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h, "m=65536,t=1,p=4") {
		t.Fatalf("default parameters changed: %q", h)
	}
	ok, err := password.Verify(h, "a passphrase")
	if err != nil || !ok {
		t.Fatalf("verify = %v, %v", ok, err)
	}
}

// Each run holds 64 MiB, so a burst of logins waits for a slot, and past the wait answers ErrBusy
// instead of taking the memory.
func TestTheHasherCapsConcurrentHashes(t *testing.T) {
	h := password.NewHasher(password.Default, 1, time.Millisecond)
	var wg sync.WaitGroup
	var ok, busy atomic.Int32
	for range 16 {
		wg.Go(func() {
			_, err := h.Hash(context.Background(), "a passphrase")
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, password.ErrBusy):
				busy.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if ok.Load() == 0 || busy.Load() == 0 {
		t.Fatalf("%d hashed, %d busy; want some of each with one slot", ok.Load(), busy.Load())
	}
}

func TestTheHasherVerifiesWhatItHashed(t *testing.T) {
	h := password.NewHasher(cheap, 2, time.Second)
	hash, err := h.Hash(context.Background(), "hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.Verify(context.Background(), hash, "hunter22"); err != nil || !ok {
		t.Fatalf("verify %v %v", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Verify(ctx, hash, "hunter22"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled caller got %v, want its own error", err)
	}
}

// A hash made at another cost is upgraded at the next login, so raising the cost reaches every account.
func TestAHashAtAnotherCostNeedsRehashing(t *testing.T) {
	h := password.NewHasher(cheap, 1, time.Second)
	current, err := h.Hash(context.Background(), "hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if h.NeedsRehash(current) {
		t.Fatalf("a hash at the hasher's own cost needs rehashing: %q", current)
	}
	with := func(change func(*password.Params)) password.Params {
		p := cheap
		change(&p)
		return p
	}
	tests := []struct {
		name   string
		params password.Params
	}{
		{"more passes", with(func(p *password.Params) { p.Time = 2 })},
		{"more memory", with(func(p *password.Params) { p.Memory *= 2 })},
		{"more lanes", with(func(p *password.Params) { p.Threads = 2 })},
		{"a shorter salt", with(func(p *password.Params) { p.SaltLen = 8 })},
		{"a longer key", with(func(p *password.Params) { p.KeyLen = 64 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old, err := password.Hash("hunter22", tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if !h.NeedsRehash(old) {
				t.Fatalf("%q does not need rehashing at %+v", old, cheap)
			}
		})
	}
	if !h.NeedsRehash("plain") {
		t.Fatal("a malformed hash does not need rehashing")
	}
}
