package item

import (
	"time"

	model "finga/internal/model/account"
)

// WardrobeResponse is the account's pieces, oldest first.
type WardrobeResponse struct {
	Pieces []PieceResponse `json:"pieces"` // never null: [] for none
}

// PieceResponse is one wardrobe entry: the piece, what gave it, the item for an item, and when.
type PieceResponse struct {
	ID      string    `json:"id"`
	Cause   string    `json:"cause"`
	ItemID  string    `json:"item_id,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

func toWardrobe(pieces []model.Piece) WardrobeResponse {
	out := WardrobeResponse{Pieces: make([]PieceResponse, 0, len(pieces))}
	for _, p := range pieces {
		r := PieceResponse{ID: p.ID, Cause: p.Cause, AddedAt: p.AddedAt}
		if p.Item != nil {
			r.ItemID = p.Item.String()
		}
		out.Pieces = append(out.Pieces, r)
	}
	return out
}
