package model

import "finga/pkg/typedid"

type (
	partyKind  struct{}
	inviteKind struct{}
)

type (
	// PartyID identifies a party.
	PartyID = typedid.ID[partyKind]
	// InviteID identifies a pending invite.
	InviteID = typedid.ID[inviteKind]
)

// NewPartyID mints a new ID.
func NewPartyID() PartyID { return typedid.New[partyKind]() }

// NewInviteID mints a new ID.
func NewInviteID() InviteID { return typedid.New[inviteKind]() }

// ParseInviteID reads an invite ID from a request; a malformed one is kernel.ErrBadID.
func ParseInviteID(s string) (InviteID, error) { return typedid.Parse[inviteKind](s) }
