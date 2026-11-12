package gamedata

import "time"

// Timing is how long an action, a basic attack or an active skill, keeps its player busy, and where in it the hit lands and the other kind may cut it off, as shares of its length.
type Timing struct {
	Length time.Duration
	Hit    float64 // 0 lands the hit at the start
	Cancel float64 // actions.yaml's share unless the action names its own
}
