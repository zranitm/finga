// Package uuidv7 mints every UUID the server makes: version 7 leads with the time, so IDs sort by creation.
package uuidv7

import (
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
