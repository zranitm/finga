package gamedata

import "time"

// Timing is how long an action, a basic attack or an active skill, keeps its player busy, and where in it the hit lands, the other kind may cut it off, and its look is back to ready, as shares of its length.
type Timing struct {
	Length time.Duration
	Hit    float64 // 0 lands the hit at the start
	Cancel float64 // actions.yaml's share unless the action names its own
	Ready  float64 // the client's look alone, as Cancel is named; the world ignores it
}

// Energy is what skills cost: a basic attack that reaches a mob gives PerAttack, up to Max.
type Energy struct {
	Max, PerAttack int
}

// Energy is actions.yaml's energy rules.
func (d *Data) Energy() Energy { return d.p.Energy }
