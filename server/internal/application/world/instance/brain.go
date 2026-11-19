package instance

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/steer"
)

// The first brain's ranges and pace: it decides every thinkEvery.
const (
	thinkEvery = 100 * time.Millisecond
	meleeShare = 0.7 // of its attack's reach, where a warrior or a thief stands
	rangedKeep = 7.5 // metres a bowman or a magician keeps from its target
	tooClose   = 3.0 // inside which it steps away
)

// brain presses a bot's keys, as a player's client would: it walks to its target and keeps its range, uses the next skill of its build's order that's ready, else attacks. It's seeded from the match, so a match plays the same each time.
type brain struct {
	rand  *rand.Rand
	order []string
	tier  gamedata.BotTier
	keys  sim.Input
	seq   uint32
	walk  steer.Walker
	stuck steer.Stall
}

// addBots posts each of the match's bots to join on the first tick, as a player joins.
func (i *Instance) addBots() {
	for _, side := range i.match.Spec.Sides {
		for _, f := range side {
			if b := f.Bot; b != nil {
				i.inbox <- &joinMsg{player: model.Player{ID: b.ID, Name: b.Name, Class: b.Class}, sink: botSink{}, done: make(chan error, 1)}
			}
		}
	}
}

// brainFor is the brain of the fighter p if they're a bot, nil for a player.
func (i *Instance) brainFor(p *player) *brain {
	f := i.match.fighters[p.ID]
	if f.bot == nil {
		return nil
	}
	var seed [2]uint64
	id, bot := i.match.Spec.ID, f.bot.ID
	seed[0], seed[1] = binary.BigEndian.Uint64(id[:8])^binary.BigEndian.Uint64(bot[8:]), binary.BigEndian.Uint64(id[8:])
	b := &brain{rand: rand.New(rand.NewPCG(seed[0], seed[1])), tier: i.tierOf(f.bot.Tier)} //nolint:gosec // a bot's choices, not a secret
	for _, build := range i.data.Pvp().Bots.Builds[f.bot.Class] {
		if equalBars(build.Bar, f.bot.Build.Bar) {
			b.order = build.Order
		}
	}
	return b
}

func equalBars(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// tierOf is the bot tier named id, the first when there's none.
func (i *Instance) tierOf(id string) gamedata.BotTier {
	tiers := i.data.Pvp().Bots.Tiers
	for _, t := range tiers {
		if t.ID == id {
			return t
		}
	}
	return tiers[0]
}

// thinkBots has each bot decide on its beat and queues its keys for the tick, as a client sends one input a tick.
func (i *Instance) thinkBots() {
	every := i.ticksFor(thinkEvery)
	for _, id := range i.order {
		p := i.players[id]
		b := p.bot
		if b == nil {
			continue
		}
		if i.tick%every == 0 {
			b.decide(i, p)
		}
		b.seq++
		p.pending = enqueueInput(p.pending, inputMsg{id: id, seq: b.seq, in: b.keys, view: uint32(i.tick)}, p.lastSeq) //nolint:gosec // a match lasts minutes
	}
}

// decide picks what the bot does until its next beat.
func (b *brain) decide(i *Instance, p *player) {
	held := b.keys
	b.keys = sim.Input{}
	if p.dead || !i.fighting() {
		return
	}
	t := i.nearestEnemy(p)
	if t == nil {
		return
	}
	from, to := nav.Point{X: p.state.X, Z: p.state.Z}, nav.Point{X: t.state.X, Z: t.state.Z}
	d := nav.Distance(from, to)
	yaw := model.YawOf(to.X-from.X, to.Z-from.Z) + float32((b.rand.Float64()*2-1)*b.tier.Aim)
	lock := model.EntityRef{Player: t.ID}
	if p.waitingCast == nil && p.action.nextStart(skillAction) <= i.tick {
		for _, id := range b.order {
			if b.ready(i, p, id, d) {
				i.casts = append(i.casts, skillMsg{id: p.ID, in: model.SkillUse{Skill: id, Root: id, Aim: yaw, Target: lock, Way: sim.Way{X: math.Sin(float64(yaw)), Z: math.Cos(float64(yaw))}}, seq: b.seq, done: make(chan error, 1)})
				break
			}
		}
	}
	if d <= reachOf(p.class.Attack.Shape) && !p.attackQueued {
		p.attackQueued, p.attackAim, p.attackTarget = true, yaw, lock
	}
	keep := rangedKeep
	if melee(p.class) {
		keep = reachOf(p.class.Attack.Shape) * meleeShare
	}
	stalled := b.stuck.Stalled(from, p.state.OnGround, held)
	switch {
	case d > keep:
		b.keys = b.walk.Keys(i.m, i.params.Radius, from, to, time.Duration(i.tick)*i.tuning.TickInterval, stalled)
	case !melee(p.class) && d < tooClose:
		b.keys = steer.KeysToward(from.X-to.X, from.Z-to.Z)
	}
}

// ready is whether the bot would use skill now at d metres from its target: learned, cooled down, paid for, and its shape reaching, or a buff not running.
func (b *brain) ready(i *Instance, p *player, id string, d float64) bool {
	s, ok := i.data.Skill(id)
	if !ok || p.skillReady[id] > i.tick {
		return false
	}
	st := s.Spec(s.PointsIn(p.Skills.Points[id]), p.Skills.Traits[id])
	if p.Energy < st.Energy {
		return false
	}
	switch s.Effect {
	case gamedata.BuffEffect:
		_, running := p.buffs[id]
		return !running
	case gamedata.DamageEffect, gamedata.StatusEffect:
		return d <= reachOf(st.Shape)
	}
	return false
}

// nearestEnemy is p's nearest enemy still up, nil for none.
func (i *Instance) nearestEnemy(p *player) *player {
	var best *player
	bestD := math.Inf(1)
	for _, id := range i.order {
		q := i.players[id]
		if q.dead || !i.enemies(p.ID, id) {
			continue
		}
		if d := math.Hypot(q.state.X-p.state.X, q.state.Z-p.state.Z); d < bestD {
			best, bestD = q, d
		}
	}
	return best
}

// melee is whether the class fights up close: its attack an arc.
func melee(c gamedata.Class) bool { return c.Attack.Shape.Kind == gamedata.ArcShape }

// reachOf is how far from its user a shape reaches.
func reachOf(s gamedata.Shape) float64 {
	switch {
	case s.Kind == gamedata.ShotShape:
		return s.Range
	case s.At == gamedata.AtLock:
		return s.Reach + s.Radius
	}
	return s.Radius
}

// botSink is a bot's: nothing it's sent goes anywhere.
type botSink struct{}

// Send drops e.
func (botSink) Send(model.Event) bool { return true }

// Stream is one that drops every frame.
func (botSink) Stream() Stream { return botStream{} }

type botStream struct{}

// SendFrame drops the frame.
func (botStream) SendFrame(*model.Frame, []model.Recipient) bool { return true }
