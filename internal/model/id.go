// Package model holds the value types shared by the application modules and
// the handlers: IDs, Class, and later Coins. It has no behaviour beyond
// parsing and printing, and imports only pkg.
package model

import (
	"github.com/google/uuid"

	"finga/pkg/errs"
)

// The ID types are distinct, so passing a character ID where an item ID is
// expected is a compile error. All are UUIDv7, generated in Go, and print
// and parse as the canonical 36-character text.
type (
	// AccountID identifies an account.
	AccountID uuid.UUID
	// CharacterID identifies a character.
	CharacterID uuid.UUID
	// ItemID identifies an item instance.
	ItemID uuid.UUID
	// SessionID identifies a login session.
	SessionID uuid.UUID
)

var errBadID = errs.Invalid("malformed ID")

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails when the random source does, which is fatal
		// for a server that mints IDs.
		panic(err)
	}
	return id
}

func parseID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, errBadID.Wrap(err)
	}
	return id, nil
}

// NewAccountID mints a new ID.
func NewAccountID() AccountID { return AccountID(newID()) }

// ParseAccountID parses the text form; bad text is invalid_argument.
func ParseAccountID(s string) (AccountID, error) {
	id, err := parseID(s)
	return AccountID(id), err
}

// String is the canonical text form.
func (id AccountID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id AccountID) IsZero() bool { return id == AccountID(uuid.Nil) }

// MarshalText implements encoding.TextMarshaler.
func (id AccountID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *AccountID) UnmarshalText(text []byte) error {
	parsed, err := ParseAccountID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// NewCharacterID mints a new ID.
func NewCharacterID() CharacterID { return CharacterID(newID()) }

// ParseCharacterID parses the text form; bad text is invalid_argument.
func ParseCharacterID(s string) (CharacterID, error) {
	id, err := parseID(s)
	return CharacterID(id), err
}

// String is the canonical text form.
func (id CharacterID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id CharacterID) IsZero() bool { return id == CharacterID(uuid.Nil) }

// MarshalText implements encoding.TextMarshaler.
func (id CharacterID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *CharacterID) UnmarshalText(text []byte) error {
	parsed, err := ParseCharacterID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// NewItemID mints a new ID.
func NewItemID() ItemID { return ItemID(newID()) }

// ParseItemID parses the text form; bad text is invalid_argument.
func ParseItemID(s string) (ItemID, error) {
	id, err := parseID(s)
	return ItemID(id), err
}

// String is the canonical text form.
func (id ItemID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id ItemID) IsZero() bool { return id == ItemID(uuid.Nil) }

// MarshalText implements encoding.TextMarshaler.
func (id ItemID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ItemID) UnmarshalText(text []byte) error {
	parsed, err := ParseItemID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// NewSessionID mints a new ID.
func NewSessionID() SessionID { return SessionID(newID()) }

// ParseSessionID parses the text form; bad text is invalid_argument.
func ParseSessionID(s string) (SessionID, error) {
	id, err := parseID(s)
	return SessionID(id), err
}

// String is the canonical text form.
func (id SessionID) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero value.
func (id SessionID) IsZero() bool { return id == SessionID(uuid.Nil) }

// MarshalText implements encoding.TextMarshaler.
func (id SessionID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *SessionID) UnmarshalText(text []byte) error {
	parsed, err := ParseSessionID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
