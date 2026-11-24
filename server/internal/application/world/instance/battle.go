package instance

import (
	"math"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// battle is a battle's score and its points as they stand (pvp.md section 7).
type battle struct {
	rules   gamedata.PvpBattle
	score   [2]int
	points  []model.CapturePoint // in the map's order
	perTick float64              // the share of a point one fighter turns in a tick
	changed bool                 // a holder, a contest or the score changed since everyone last heard
}

func newBattle(rules gamedata.PvpBattle, points []gamedata.CapturePoint, tick time.Duration) *battle {
	b := &battle{rules: rules, perTick: float64(tick) / float64(rules.Capture.Time)}
	for _, p := range points {
		b.points = append(b.points, model.CapturePoint{ID: p.ID})
	}
	return b
}

// leader is the side ahead, 0 for an even score.
func (b *battle) leader() int {
	switch {
	case b.score[0] > b.score[1]:
		return 1
	case b.score[1] > b.score[0]:
		return 2
	}
	return 0
}

// stepBattle runs a battle's tick while it's fought: each point turns, and once a second each side scores its points and heals its own in its base. The first side to the goal wins; everyone hears the score each second and at each change.
func (i *Instance) stepBattle() {
	mt := i.match
	if mt == nil || mt.battle == nil || mt.phase != model.PhaseFight {
		return
	}
	b := mt.battle
	i.capture()
	if i.tick%i.ticksFor(time.Second) == 0 {
		for _, pt := range b.points {
			if pt.Holder > 0 {
				b.score[pt.Holder-1] += b.rules.PerPoint
			}
		}
		i.baseHeal()
		b.changed = true
	}
	if b.score[0] >= b.rules.Goal || b.score[1] >= b.rules.Goal {
		i.endMatch(b.leader())
		return
	}
	if b.changed {
		b.changed = false
		i.events = append(i.events, i.matchState())
	}
}

// capture turns each point toward the side alone in it, faster for each more of them up to fastest; one held by the other side drains to no one's on the way, and full progress makes it the side's. With both sides in it, it holds still.
func (i *Instance) capture() {
	b := i.match.battle
	c := b.rules.Capture
	for k, at := range i.m.Points {
		var in [2]int
		for _, id := range i.order {
			if p := i.players[id]; !p.dead && at.Holds(p.state.X, p.state.Z) {
				in[i.side(id)-1]++
			}
		}
		pt := &b.points[k]
		was := *pt
		pt.Contested = in[0] > 0 && in[1] > 0
		if s := alone(in); s > 0 {
			end := float64(2*s - 3) // side 1's -1, side 2's 1
			step := float64(min(1+float64(c.PerExtra*float64(in[s-1]-1)), c.Fastest) * b.perTick)
			if end < 0 {
				pt.Progress = max(pt.Progress-step, end)
			} else {
				pt.Progress = min(pt.Progress+step, end)
			}
			if pt.Holder != 0 && pt.Holder != s && pt.Progress*end >= 0 {
				pt.Holder = 0
			}
			if pt.Progress == end {
				pt.Holder = s
			}
		}
		b.changed = b.changed || pt.Holder != was.Holder || pt.Contested != was.Contested
	}
}

// alone is the side with fighters in a point when the other has none, 0 when neither or both do.
func alone(in [2]int) int {
	switch {
	case in[0] > 0 && in[1] == 0:
		return 1
	case in[1] > 0 && in[0] == 0:
		return 2
	}
	return 0
}

// baseHeal gives each living fighter in their own base a second of its heal.
func (i *Instance) baseHeal() {
	b := i.match.battle
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || !i.m.Bases[i.side(id)-1].Holds(p.state.X, p.state.Z) {
			continue
		}
		if regained(&p.HP, &p.hpCarry, float64(b.rules.BaseHeal*float64(p.derived.MaxHP)), p.derived.MaxHP) {
			p.changed = true
		}
	}
}

// downFor is the tick a fighter downed now gets up: after the respawn in a battle, never in a round, whose reset brings them back.
func (i *Instance) downFor() uint64 {
	if b := i.match.battle; b != nil {
		return i.tick + i.ticksFor(b.rules.Respawn)
	}
	return math.MaxUint64
}

// scoreDown gives a battle's down to the other side: only an enemy's hit, or the damage over time it left, takes a fighter down, so the last hit is always theirs.
func (i *Instance) scoreDown(p *player) {
	if b := i.match.battle; b != nil {
		b.score[1-i.match.fighters[p.ID].side] += b.rules.PerDown
		b.changed = true
	}
}

// backInBase brings a battle's downed fighter up at full HP at their base's spot farthest from any body, wearing the build they saved since they last put one on; their cooldowns ran on.
func (i *Instance) backInBase(p *player) {
	f := i.match.fighters[p.ID]
	spot := i.freestSpot(f.side, p)
	i.wearBuild(p.ID, f)
	p.dead = false
	p.state = i.standing(spot.At.X, 0, spot.At.Z, float32(spot.Yaw))
	p.aim = float32(spot.Yaw)
	p.HP, p.hpCarry = p.derived.MaxHP, 0
	p.changed, p.moved = true, true
	i.events = append(i.events, model.Respawned{Tick: i.tick, ID: p.ID, MapID: i.m.ID, X: p.state.X, Y: p.state.Y, Z: p.state.Z, HP: p.HP, Yaw: float32(spot.Yaw)})
}

// freestSpot is side's starting spot whose nearest body, p aside, is farthest; the first on a tie.
func (i *Instance) freestSpot(side int, p *player) gamedata.Spot {
	spots := i.m.Sides[side]
	best, far := spots[0], -1.0
	for _, s := range spots {
		near := math.Inf(1)
		for _, id := range i.order {
			if q := i.players[id]; q != p {
				near = min(near, math.Hypot(q.state.X-s.At.X, q.state.Z-s.At.Z))
			}
		}
		if near > far {
			best, far = s, near
		}
	}
	return best
}

// view writes the battle's part of the match as its fighters see it.
func (b *battle) view(st *model.MatchState) {
	st.Battle, st.Score, st.Points = true, b.score, slices.Clone(b.points)
}
