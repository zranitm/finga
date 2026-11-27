package e2etest

import (
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

// walked is a recording of a player walking from tick start, in combat from each hit until combat ticks after it, with the pace the world gives each tick; cut keeps the count from ever rising, reset ends it at the tick.
func walked(start, last uint64, hits []uint64, cut bool, reset uint64) *Recording {
	r := &Recording{Hits: hits}
	counted := 0
	for t := start; t <= last; t++ {
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
		r.Paces = append(r.Paces, Pace{Tick: t, Sprint: counted, Speed: speed})
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
