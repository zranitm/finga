// Package uuidv7 mints every UUID the server makes: version 7 leads with the time, so IDs sort by creation.
package uuidv7

import "github.com/google/uuid"

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
