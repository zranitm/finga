// Package model holds the value types shared by every service: IDs, Class,
// and later Coins. It has no behaviour beyond parsing and printing, and
// imports only pkg.
package model

import "finga/pkg/typedid"

// The kinds behind the ID types. They are never used as values.
type (
	accountKind   struct{}
	characterKind struct{}
	itemKind      struct{}
	sessionKind   struct{}
)

// The ID types are distinct (pkg/typedid), so passing a character ID where
// an item ID is expected is a compile error. All are UUIDv7, generated in
// Go, and print and parse as the canonical 36-character text.
type (
	// AccountID identifies an account.
	AccountID = typedid.ID[accountKind]
	// CharacterID identifies a character.
	CharacterID = typedid.ID[characterKind]
	// ItemID identifies an item instance.
	ItemID = typedid.ID[itemKind]
	// SessionID identifies a login session.
	SessionID = typedid.ID[sessionKind]
)

// ErrBadID is a malformed ID in a request.
var ErrBadID = typedid.ErrMalformed

// NewAccountID mints a new ID.
func NewAccountID() AccountID { return typedid.New[accountKind]() }

// ParseAccountID parses the text form; bad text is ErrBadID.
func ParseAccountID(s string) (AccountID, error) { return typedid.Parse[accountKind](s) }

// NewCharacterID mints a new ID.
func NewCharacterID() CharacterID { return typedid.New[characterKind]() }

// ParseCharacterID parses the text form; bad text is ErrBadID.
func ParseCharacterID(s string) (CharacterID, error) { return typedid.Parse[characterKind](s) }

// NewItemID mints a new ID.
func NewItemID() ItemID { return typedid.New[itemKind]() }

// ParseItemID parses the text form; bad text is ErrBadID.
func ParseItemID(s string) (ItemID, error) { return typedid.Parse[itemKind](s) }

// NewSessionID mints a new ID.
func NewSessionID() SessionID { return typedid.New[sessionKind]() }

// ParseSessionID parses the text form; bad text is ErrBadID.
func ParseSessionID(s string) (SessionID, error) { return typedid.Parse[sessionKind](s) }
