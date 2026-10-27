// Package typedid makes typed identifiers over UUIDv7. ID is generic over a
// kind, an otherwise unused type, so each kind of ID is its own Go type:
// passing an order ID where a customer ID is expected does not compile.
//
//	type orderKind struct{}
//	type OrderID = typedid.ID[orderKind]
//
// IDs are minted in Go (version 7, so they sort in creation order), print
// as the canonical 36-character text, and marshal as that text.
// They convert to and from uuid.UUID with a plain conversion.
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

// New mints an ID. It panics only when the system's random source fails,
// which no program that mints IDs can recover from.
func New[K any]() ID[K] {
	u, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return ID[K](u)
}

// Parse reads the text form (the canonical one, and the other forms
// google/uuid accepts); anything else is ErrMalformed.
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

// truncate keeps an error message short whatever a caller sent.
func truncate(s string) string {
	const limit = 40
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
