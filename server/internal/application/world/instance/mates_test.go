package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// On a map cut by cells, a party's members hear once a second where the others on it stand that they don't see; once all are in sight again, once more with none.
func TestAPartyMateOutOfSightIsSpottedOnceASecond(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cell := data.Interest().Cell
		ayla, robin := worldtest.NewPlayer("Ayla", 20, 20), worldtest.NewPlayer("Robin", 20, 5*cell+16)        // rows 0 and 5, out of each other's blocks
		parties := &fakeParties{parties: [][]kernel.CharacterID{{ayla.ID, robin.ID, kernel.NewCharacterID()}}} // the third on another map
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{Parties: parties})
		defer stop()
		sa, sr := join(t, inst, ayla), join(t, inst, robin)
		second := int(time.Second / worldtest.TickInterval)
		tick(second)
		told := eventsOf[model.PartySpots](sa)
		for _, s := range append(told, eventsOf[model.PartySpots](sr)...) {
			if len(s.Spots) != 1 {
				t.Fatalf("spots %+v; want the one mate out of sight on the map", s.Spots)
			}
		}
		if len(told) != 1 || told[0].Spots[0] != (model.PartySpot{ID: robin.ID, X: robin.X, Z: robin.Z}) || len(eventsOf[model.PartySpots](sr)) != 1 {
			t.Fatalf("Ayla told %+v, Robin told %d times in a second; want each the other, where they stand, once", told, len(eventsOf[model.PartySpots](sr)))
		}
		p, _ := sr.Own(robin.ID)
		for seq := uint32(0); p.State.Z >= 4*cell; p, _ = sr.Own(robin.ID) { // into row 3, the last of her block
			seq = worldtest.Walk(inst, robin.ID, seq, sim.Input{Down: true}, 1)
		}
		before := len(eventsOf[model.PartySpots](sa))
		tick(2 * second)
		after := eventsOf[model.PartySpots](sa)[before:]
		if len(after) != 1 || len(after[0].Spots) != 0 {
			t.Fatalf("Ayla told %+v in two seconds with Robin in sight; want once, with no one", after)
		}
	})
}

// On a map sent whole every player sees every other, so nobody is told a spot.
func TestAMapSentWholeTellsNoSpots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ayla, robin := worldtest.NewPlayer("Ayla", 5, 5), worldtest.NewPlayer("Robin", 35, 35)
		parties := &fakeParties{parties: [][]kernel.CharacterID{{ayla.ID, robin.ID}}}
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		s := join(t, inst, ayla)
		join(t, inst, robin)
		tick(2 * int(time.Second/worldtest.TickInterval))
		if told := eventsOf[model.PartySpots](s); len(told) != 0 {
			t.Fatalf("told %+v on a map sent whole", told)
		}
	})
}
