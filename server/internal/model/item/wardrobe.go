package item

import (
	"time"

	"finga/internal/shared/kernel"
)

// PieceCause says what gave an account a piece.
type PieceCause string

// The piece causes; ranks, quests and feats come with their sources (outfits.md section 10, step 4).
const PieceFromItem PieceCause = "item"

// Piece is a wardrobe entry: a piece the account collected, what gave it, and when.
type Piece struct {
	ID      string
	Cause   PieceCause
	Item    *kernel.ItemID // the item added, for an item alone
	AddedAt time.Time
}

// Look is a character's four picks, each a piece id, "" keeping the class's own, at the count of its changes.
type Look struct {
	Outfit       string
	Headwear     string
	Face         string
	Weapon       string
	HideHeadwear bool
	Revision     uint64 // 0 for a character that never set one
}
