package gamedata

import "time"

// Timing is how long an action, a basic attack or an active skill, keeps its player busy, and where in it the hit lands, the other kind may cut it off, and its look is back to ready, as shares of its length.
type Timing struct {
	Length time.Duration
	Hit    float64 // 0 lands the hit at the start
	Cancel float64 // actions.yaml's share unless the action names its own
	Ready  float64 // the client's look alone, as Cancel is named; the world ignores it
}

// Energy is what skills cost: a basic attack that reaches a mob gives PerAttack, and a block use's first blocked attack PerBlock, up to Max.
type Energy struct {
	Max, PerAttack, PerBlock int
}

// Block is what a block covers: Arc degrees round its user, centred on its aim. Front is cos(Arc/2): a blow is from the front when the aim's dot product with the way it comes from is at least this.
type Block struct {
	Arc, Front float64
}

// Block is actions.yaml's block rules.
func (d *Data) Block() Block { return d.p.Block }

// Energy is actions.yaml's energy rules.
func (d *Data) Energy() Energy { return d.p.Energy }

// Combat is how long a player stays in combat after their last fight event: a hit, a miss, a dodge or damage over time, dealt or taken.
func (d *Data) Combat() time.Duration { return d.p.Combat }
