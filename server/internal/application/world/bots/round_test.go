package bots

import (
	"testing"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// pvp.md section 19: a bot walking north that meets an enemy it isn't after steps round it, on the side the enemy isn't on; its target, a teammate, one behind, one far off or a downed one change nothing.
func TestABotStepsRoundAnEnemyInItsWay(t *testing.T) {
	me := Self{ID: kernel.NewCharacterID(), Side: 1, State: sim.State{X: 10, Z: 10}}
	enemy := func(dx, dz float64) Seen {
		return Seen{ID: kernel.NewCharacterID(), Side: 2, Up: true, Body: model.Body{X: 10 + dx, Z: 10 + dz}}
	}
	north := sim.Input{Up: true}
	b := &Brain{}
	for _, c := range []struct {
		name string
		in   []Seen
		want func(sim.Input) bool
	}{
		{"ahead, a little to the east", []Seen{enemy(0.3, 1.2)}, func(k sim.Input) bool { return k.Left && !k.Right }},
		{"ahead, a little to the west", []Seen{enemy(-0.3, 1.2)}, func(k sim.Input) bool { return k.Right && !k.Left }},
		{"ahead, on the line", []Seen{enemy(0, 1.2)}, func(k sim.Input) bool { return k.Left != k.Right }},
		{"far ahead", []Seen{enemy(0, 3)}, func(k sim.Input) bool { return k == north }},
		{"behind", []Seen{enemy(0, -1.2)}, func(k sim.Input) bool { return k == north }},
		{"off to the side", []Seen{enemy(1.5, 0)}, func(k sim.Input) bool { return k == north }},
		{"a teammate ahead", []Seen{{ID: kernel.NewCharacterID(), Side: 1, Up: true, Body: model.Body{X: 10, Z: 11.2}}}, func(k sim.Input) bool { return k == north }},
		{"a downed enemy ahead", []Seen{{ID: kernel.NewCharacterID(), Side: 2, Up: false, Body: model.Body{X: 10, Z: 11.2}}}, func(k sim.Input) bool { return k == north }},
	} {
		if got := b.round(me, north, c.in, kernel.CharacterID{}); !c.want(got) {
			t.Errorf("%s: the keys %+v from north", c.name, got)
		}
	}
	target := enemy(0.3, 1.2)
	if got := b.round(me, north, []Seen{target}, target.ID); got != north {
		t.Errorf("the target ahead: keys %+v; want north on, to close on it", got)
	}
	if got := b.round(me, sim.Input{}, []Seen{enemy(0, 1)}, kernel.CharacterID{}); got != (sim.Input{}) {
		t.Errorf("standing still: keys %+v", got)
	}
}
