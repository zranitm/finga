// Package password hashes passwords with argon2id in PHC format.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params tune argon2id. Memory is in KiB.
type Params struct {
	Time    uint32
	Memory  uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// Default is the production setting for interactive logins: 64 MiB, one pass, four lanes.
var Default = Params{Time: 1, Memory: 64 * 1024, Threads: 4, SaltLen: 16, KeyLen: 32}

const (
	algorithm = "argon2id"
	phcFields = 6 // "", argon2id, v=19, m=..,t=..,p=.., salt, hash
)

var (
	errMalformed = errors.New("password: malformed hash")
	enc          = base64.RawStdEncoding
)

// Hash derives a salted hash: $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>.
func Hash(password string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s", algorithm, argon2.Version, p.Memory, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports in constant time whether password matches; an error means a bad hash.
func Verify(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != phcFields || parts[0] != "" || parts[1] != algorithm {
		return false, errMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformed
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, errMalformed
	}
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, errMalformed
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, errMalformed
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want))) //nolint:gosec // length of a decoded hash
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
