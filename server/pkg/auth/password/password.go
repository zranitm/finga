// Package password hashes passwords with argon2id in PHC format.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sync/semaphore"
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
	d, err := decode(hash)
	if err != nil {
		return false, err
	}
	p := d.params
	got := argon2.IDKey([]byte(password), d.salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return subtle.ConstantTimeCompare(got, d.key) == 1, nil
}

// decoded is a PHC string's parts, its salt and key lengths in params.
type decoded struct {
	params    Params
	salt, key []byte
}

func decode(hash string) (decoded, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != phcFields || parts[0] != "" || parts[1] != algorithm {
		return decoded{}, errMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return decoded{}, errMalformed
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return decoded{}, errMalformed
	}
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return decoded{}, errMalformed
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return decoded{}, errMalformed
	}
	p.SaltLen, p.KeyLen = uint32(len(salt)), uint32(len(key)) //nolint:gosec // lengths of a decoded hash
	return decoded{params: p, salt: salt, key: key}, nil
}

// ErrBusy is a hash that found every slot taken for its whole wait.
var ErrBusy = errors.New("password: too many hashes at once")

// Hasher runs Hash and Verify at most slots at a time: each run holds Params.Memory, so the slots bound
// what a burst of logins can take. A call waits up to wait for a slot, then fails with ErrBusy.
type Hasher struct {
	params Params
	slots  *semaphore.Weighted
	wait   time.Duration
}

// NewHasher hashes with p, slots at a time.
func NewHasher(p Params, slots int64, wait time.Duration) *Hasher {
	return &Hasher{params: p, slots: semaphore.NewWeighted(slots), wait: wait}
}

// Hash is Hash with the hasher's params, once a slot is free.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.slots.Release(1)
	return Hash(password, h.params)
}

// Verify is Verify, once a slot is free.
func (h *Hasher) Verify(ctx context.Context, hash, password string) (bool, error) {
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.slots.Release(1)
	return Verify(hash, password)
}

// NeedsRehash reports whether hash was made with other params than the hasher's, or cannot be read.
func (h *Hasher) NeedsRehash(hash string) bool {
	d, err := decode(hash)
	return err != nil || d.params != h.params
}

// acquire takes a slot within the wait; a caller whose own context ended gets its error.
func (h *Hasher) acquire(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, h.wait)
	defer cancel()
	if err := h.slots.Acquire(waitCtx, 1); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrBusy
	}
	return nil
}
