// Package uuidv7 mints every UUID the server makes: version 7 leads with the time, so IDs sort by creation.
package uuidv7

import (
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
)

// errNotV7 is text that is not a version 7 UUID.
var errNotV7 = errors.New("uuidv7: not a version 7 UUID")

// New is a fresh UUIDv7; it panics only if the system's random source fails.
func New() uuid.UUID {
	u, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return u
}

// Derived is a UUIDv7 fixed by base and name: base's time, then a hash of both, so deriving it again, after a crash too, gives the same one, as an idempotency key needs.
func Derived(base uuid.UUID, name string) uuid.UUID {
	sum := sha256.Sum256(append(base[:], name...))
	var u uuid.UUID
	copy(u[:6], base[:6]) // the 48-bit millisecond time
	copy(u[6:], sum[:10])
	u[6] = u[6]&0x0f | 0x70 // version 7
	u[8] = u[8]&0x3f | 0x80 // RFC 4122 variant
	return u
}

// NewString is New in its canonical text form.
func NewString() string { return New().String() }

// Parse reads a version 7 UUID in any form google/uuid accepts; anything else, the nil UUID included, is an error.
func Parse(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil || u.Version() != 7 {
		return uuid.UUID{}, errNotV7
	}
	return u, nil
}
