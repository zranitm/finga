package item

import (
	"testing"

	"finga/internal/application/item"
	"finga/pkg/copytest"
)

// A wardrobe reaches the client with every field of every piece.
func TestAWardrobeKeepsItsFields(t *testing.T) {
	match := copytest.Match{Renames: map[string]string{"Item": "ItemID"}}
	if got := copytest.Diff(match, func(pieces []item.Piece) []PieceResponse { return toWardrobe(pieces).Pieces }); len(got) > 0 {
		t.Error(got)
	}
}
