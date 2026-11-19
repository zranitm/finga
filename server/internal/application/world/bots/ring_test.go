package bots_test

import (
	"testing"

	"finga/internal/application/world/bots"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// The ring gives the fighters as they were at a tick, the oldest it keeps when asked further back, and the actions shown by then whose hit or next blow is still to come; an action is dropped once a ring has passed both.
func TestTheRingGivesTheFightersAsTheyWere(t *testing.T) {
	r := bots.NewRing(4)
	id := kernel.NewCharacterID()
	at := func(tick uint64) []bots.Seen { return []bots.Seen{{ID: id, Body: model.Body{X: float64(tick)}}} }
	for tick := uint64(10); tick <= 15; tick++ {
		r.Record(tick, at(tick))
	}
	if seen, _ := r.As(13, 15); len(seen) != 1 || seen[0].Body.X != 13 {
		t.Fatalf("as of 13: %+v", seen)
	}
	if seen, _ := r.As(5, 15); len(seen) != 1 || seen[0].Body.X != 12 {
		t.Fatalf("as of 5, before the ring: %+v, want its oldest, 12", seen)
	}

	r.Start(bots.Started{By: id, Shown: 14, Start: 14, Hit: 14, Next: 18})
	r.Start(bots.Started{By: id, Shown: 12, Start: 12, Hit: 12, Next: 13})
	if _, started := r.As(13, 15); len(started) != 0 {
		t.Fatalf("as of 13 at 15: %+v, want none: one not yet shown, one past", started)
	}
	if _, started := r.As(14, 15); len(started) != 1 || started[0].Next != 18 {
		t.Fatalf("as of 14 at 15: %+v, want the one shown at 14", started)
	}
	r.Record(23, at(23))
	if _, started := r.As(23, 18); len(started) != 0 {
		t.Fatalf("a ring past its next blow: %+v kept", started)
	}
}
