// Package kernel holds the value types shared by every service. It imports only pkg.
package kernel

import "finga/pkg/typedid"

// Phantom kinds for the ID types.
type (
	accountKind   struct{}
	characterKind struct{}
	itemKind      struct{}
	sessionKind   struct{}
)

// Distinct UUIDv7 ID types: passing one kind where another is expected does not compile.
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
