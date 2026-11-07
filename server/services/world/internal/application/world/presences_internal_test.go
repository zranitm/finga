package world

import (
	"testing"

	"finga/internal/kernel"
	"finga/services/world/internal/application/instance"
)

// A map drops a player whose sink failed; the registry then forgets them, unless a newer join holds the entry or a transfer moved it elsewhere.
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
		{"a transfer that reached this map before its landing marked it", presence{inst: town, conn: "c-1", transit: true}, false},
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

// The map that holds a player hands them on even while their landing there has not yet marked them landed; that landing then marks nothing.
func TestAHandOffComesFromTheMapThatHoldsThePlayer(t *testing.T) {
	town, forest, cave := &instance.Instance{}, &instance.Instance{}, &instance.Instance{}
	tests := []struct {
		name   string
		held   presence
		from   *instance.Instance
		handed bool
	}{
		{"from the map it stands on", presence{inst: forest, conn: "c-1", gen: 1}, forest, true},
		{"from the map it is landing on, before the landing marked it", presence{inst: forest, conn: "c-1", gen: 1, transit: true}, forest, true},
		{"from a map that no longer holds it", presence{inst: cave, conn: "c-1", gen: 1}, forest, false},
		{"through another connection", presence{inst: forest, conn: "c-2", gen: 1}, forest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := kernel.NewCharacterID()
			ps := &presences{by: map[kernel.CharacterID]presence{id: tt.held}, next: 1}
			at, ok := ps.handOff(id, "c-1", tt.from, town)
			if ok != tt.handed {
				t.Fatalf("handed off %v, want %v", ok, tt.handed)
			}
			if !ok {
				if ps.by[id] != tt.held {
					t.Fatalf("a refused hand-off changed the presence to %+v", ps.by[id])
				}
				return
			}
			if at.inst != town || !at.transit || at.gen == tt.held.gen {
				t.Fatalf("handed off as %+v; want in transit to town, stamped anew", at)
			}
			if ps.landed(id, tt.held.gen) {
				t.Fatal("the earlier landing marked the presence it no longer owns")
			}
			if !ps.landed(id, at.gen) || ps.by[id].transit {
				t.Fatal("the hand-off's own landing did not mark it landed")
			}
		})
	}
}
