package world

import (
	"finga/services/world/internal/application/instance"
	"testing"

	"finga/internal/kernel"
)

// A map drops a player whose sink failed; the registry then forgets them, unless a newer join holds the entry or a transfer moved it.
func TestDropForgetsOnlyTheLivePresenceItNames(t *testing.T) {
	town, cave := &instance.Instance{}, &instance.Instance{}
	tests := []struct {
		name string
		held presence
		kept bool
	}{
		{"the presence the map dropped", presence{inst: town, conn: "c-1"}, false},
		{"a newer join through another connection", presence{inst: town, conn: "c-2"}, true},
		{"a transfer that moved it to another map", presence{inst: cave, conn: "c-1"}, true},
		{"a transfer still on its way, to this map", presence{inst: town, conn: "c-1", transit: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := kernel.NewCharacterID()
			ps := &presences{by: map[kernel.CharacterID]presence{id: tt.held}}
			ps.drop(id, town, "c-1")
			if _, kept := ps.by[id]; kept != tt.kept {
				t.Fatalf("kept %v, want %v", kept, tt.kept)
			}
		})
	}
}
