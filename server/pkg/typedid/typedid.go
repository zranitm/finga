// Package typedid makes UUIDv7 IDs typed by kind, so mixing kinds does not compile.
package typedid

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ID is an identifier of kind K.
type ID[K any] uuid.UUID

// ErrMalformed is text that is not an ID.
var ErrMalformed = errors.New("typedid: malformed ID")

// New mints an ID; it panics only if the system's random source fails.
func New[K any]() ID[K] {
	u, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return ID[K](u)
}

// Parse reads any text form google/uuid accepts; anything else is ErrMalformed.
func Parse[K any](s string) (ID[K], error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ID[K]{}, fmt.Errorf("%w: %q", ErrMalformed, truncate(s))
	}
	return ID[K](u), nil
}

// String is the canonical text form.
func (id ID[K]) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id ID[K]) IsZero() bool { return id == ID[K]{} }

// MarshalText implements encoding.TextMarshaler.
func (id ID[K]) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID[K]) UnmarshalText(text []byte) error {
	parsed, err := Parse[K](string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func truncate(s string) string {
	const limit = 40
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
