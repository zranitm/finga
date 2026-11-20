package world

import (
	"testing"

	"finga/internal/shared/kernel"
)

// A player is in the match they last entered: the end of an earlier one, its stop landing late, doesn't take them out of a later one.
func TestAPlayerIsInTheMatchTheyLastEntered(t *testing.T) {
	var f fighting
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	f.enter("a", []kernel.CharacterID{ayla, robin})
	f.enter("b", []kernel.CharacterID{ayla})
	f.leave("a", []kernel.CharacterID{ayla, robin})
	if id, ok := f.of(ayla); !ok || id != "b" {
		t.Fatalf("Ayla in %q, %v; want the later match", id, ok)
	}
	if _, ok := f.of(robin); ok {
		t.Fatal("Robin is still in the match that ended")
	}
}
