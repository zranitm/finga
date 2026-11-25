package item

import (
	"time"

	"finga/internal/shared/kernel"
)

// PieceCause says what gave an account a piece.
type PieceCause string

// The causes: an item added, a PvP rank's piece the first time the account reached it, or a quest's piece with its hand-in; feats come with their source (outfits.md section 10, step 4).
const (
	PieceFromItem  PieceCause = "item"
	PieceFromRank  PieceCause = "rank"
	PieceFromQuest PieceCause = "quest"
)

// PieceGrant is a piece given outright for Cause, Ref naming which rank or quest.
type PieceGrant struct {
	Piece string
	Cause PieceCause
	Ref   string
}

// Piece is a wardrobe entry: a piece the account collected, what gave it, and when.
type Piece struct {
	ID      string
	Cause   PieceCause
	Item    *kernel.ItemID // the item added, for an item alone
	AddedAt time.Time
}

// Look is a character's five picks, each a piece id, "" keeping the class's own, at the count of its changes.
type Look struct {
	Outfit       string
	Headwear     string
	Face         string
	Weapon       string
	Wings        string
	HideHeadwear bool
	Revision     uint64 // 0 for a character that never set one
}
