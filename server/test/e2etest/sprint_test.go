package e2etest

import (
	"slices"
	"strings"
	"testing"
)

const (
	perTick = 3   // sub-steps a tick
	steps   = 120 // sub-steps of walking to a sprint
	walk    = 5.0
	mult    = 1.6
	combat  = 100
)

// walked is a recording of a player walking from tick start, in combat from each hit until combat ticks after it, with the pace the world gives each tick; cut keeps the count from ever rising, reset ends it at the tick, and a tick in skip has no frame and no input, as on a starved runner whose merged frames the player answers once.
func walked(start, last uint64, hits []uint64, cut bool, reset uint64, skip ...uint64) *Recording {
	r := &Recording{Hits: hits}
	counted, ack := 0, uint32(0)
	for t := start; t <= last; t++ {
		if slices.Contains(skip, t) {
			continue
		}
		ack++
		fighting := false
		for _, h := range hits {
			fighting = fighting || (t > h && t < h+combat)
		}
		speed := walk
		switch {
		case fighting || cut || t == reset:
			counted = 0
		default:
			if counted >= steps {
				speed = walk * mult
			}
			counted = min(counted+perTick, steps)
		}
		r.Paces = append(r.Paces, Pace{Tick: t, Ack: ack, Sprint: counted, Speed: speed})
	}
	return r
}

// without drops the frame at tick from r, whose input the server applied all the same.
func without(r *Recording, tick uint64) *Recording {
	r.Paces = slices.DeleteFunc(r.Paces, func(p Pace) bool { return p.Tick == tick })
	return r
}

// lagging is r with the count a tick late at the end, as when the walk's first tick is counted as the player's own.
func lagging(r *Recording) *Recording {
	for i := range r.Paces {
		r.Paces[i].Sprint = max(0, r.Paces[i].Sprint-perTick)
	}
	return r
}

// The frames of a walk out of combat that count and sprint as section 7 says have no fault; the same with the sprint path cut, a reset of the count part way, or a sprint in combat do.
func TestARecordedWalkIsJudgedAgainstTheSprintRules(t *testing.T) {
	for _, tc := range []struct {
		name  string
		r     *Recording
		fault string
	}{
		{"a walk out of combat", walked(10, 80, nil, false, 0), ""},
		{"a walk that ends before the sprint", walked(10, 40, nil, false, 0), "no frame showed the sprint"},
		{"the sprint path cut", walked(10, 80, nil, true, 0), "a sprint count of 0"},
		{"a count reset part way", walked(10, 120, nil, false, 50), "a sprint count of"},
		{"a walk through a hit's combat, sprinting after it", walked(10, 200, []uint64{20}, false, 0), ""},
		{"a hit's combat that never ends in the frames", walked(10, 140, []uint64{20}, false, 0), "no frame showed the sprint"},
		{"a walk with frames skipped, each leaving an input out", walked(10, 120, nil, false, 0, 30, 41, 42, 70), ""},
		{"frames skipped through a hit's combat, sprinting after it", walked(10, 220, []uint64{20}, false, 0, 15, 119, 130, 150, 151), ""},
		{"the frame at combat's last tick lost, its input applied", without(walked(10, 220, []uint64{20}, false, 0), 119), ""},
		{"a count that lags behind the inputs with frames skipped", lagging(walked(10, 120, nil, false, 0, 30, 41, 42, 70)), "a sprint count of"},
		{"the sprint path cut after a hit", walked(10, 200, []uint64{20}, true, 0), "a sprint count of 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.r.Fault(combat, perTick, steps, mult)
			if (tc.fault == "") != (got == "") || !strings.Contains(got, tc.fault) {
				t.Fatalf("fault %q; want one holding %q", got, tc.fault)
			}
		})
	}
	sprintInCombat := walked(10, 200, []uint64{20}, false, 0)
	sprintInCombat.Paces[50] = Pace{Tick: 60, Sprint: 120, Speed: walk * mult}
	if got := sprintInCombat.Fault(combat, perTick, steps, mult); !strings.Contains(got, "in combat") {
		t.Fatalf("a sprint count in combat: fault %q", got)
	}
	slow := walked(10, 80, nil, false, 0)
	slow.Paces[len(slow.Paces)-1].Speed = walk
	if got := slow.Fault(combat, perTick, steps, mult); !strings.Contains(got, "at 5.0000 m/s") {
		t.Fatalf("a sprint that stays at the walk: fault %q", got)
	}
}
