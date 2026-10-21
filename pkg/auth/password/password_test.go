package password_test

import (
	"strings"
	"testing"

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
	// $argon2id$v=19$m=8192,t=1,p=1$<salt>$<hash>
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
