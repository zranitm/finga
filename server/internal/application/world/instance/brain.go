package instance

import (
	"encoding/binary"
	"slices"
	"time"

	"finga/internal/application/world/bots"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/sim"
)

// thinkEvery is how often a bot decides: every other tick.
const thinkEvery = 100 * time.Millisecond

// bot is a match's bot's: its brain, the keys it holds, and the sequence its inputs count.
type bot struct {
	brain *bots.Brain
	keys  sim.Input
	seq   uint32
}

// addBots posts each of the match's bots to join on the first tick, as a player joins.
func (i *Instance) addBots() {
	for _, side := range i.match.Spec.Sides {
		for _, f := range side {
			if b := f.Bot; b != nil {
				class, _ := i.data.Class(b.Class)
				look := model.BotLook(i.data.Pieces(), class.Weapon, b.ID)
				i.inbox <- &joinMsg{player: model.Player{ID: b.ID, Name: b.Name, Class: b.Class, Look: look}, sink: botSink{}, done: make(chan error, 1)}
			}
		}
	}
}

// botFor is the fighter p's bot if they're one, nil for a player: its brain seeded from the match and the bot, with the kit's skills p holds.
func (i *Instance) botFor(p *player) *bot {
	f := i.match.fighters[p.ID]
	if f.bot == nil {
		return nil
	}
	id, b := i.match.Spec.ID, f.bot.ID
	setup := bots.Setup{
		Seed: [2]uint64{binary.BigEndian.Uint64(id[:8]) ^ binary.BigEndian.Uint64(b[8:]), binary.BigEndian.Uint64(id[8:])},
		Tier: i.tierOf(f.bot.Tier), Class: p.class, Bar: f.bot.Build.Bar, Skills: map[string]bots.Skill{},
		Map: i.m, Radius: i.params.Radius, Height: i.params.Height, Tick: i.tuning.TickInterval,
	}
	for _, build := range i.data.Pvp().Bots.Builds[f.bot.Class] {
		if slices.Equal(build.Bar, f.bot.Build.Bar) {
			setup.Order = build.Order
		}
	}
	for _, s := range i.data.Skills() {
		if points := s.PointsIn(p.Skills.Points[s.ID]); s.Kind == gamedata.Active && points > 0 && p.class.Learnable(s) {
			trait := p.Skills.Traits[s.ID]
			if k, ok := s.Trait(trait); !ok || k == 0 || !s.TraitOpen(points) {
				trait = ""
			}
			setup.Skills[s.ID] = bots.Skill{Skill: s, Spec: s.Spec(points, trait)}
		}
	}
	return &bot{brain: bots.New(setup)}
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

// thinkBots keeps the match's fighters in its ring, has each bot decide on its beat, and queues its keys for the tick, as a client sends one input a tick; a match with no bots keeps no ring.
func (i *Instance) thinkBots() {
	mt := i.match
	if mt == nil || mt.ring == nil {
		return
	}
	mt.seen = mt.seen[:0]
	for _, id := range i.order {
		p := i.players[id]
		mt.seen = append(mt.seen, bots.Seen{ID: id, Side: i.side(id), Body: i.bodyOf(p), HP: p.HP, MaxHP: p.derived.MaxHP, Up: !p.dead && p.HP > 0, Hard: p.statuses.Hard(), Attack: p.class.Attack.Shape})
	}
	mt.ring.Record(i.tick, mt.seen)
	every := i.ticksFor(thinkEvery)
	for _, id := range i.order {
		p := i.players[id]
		b := p.bot
		if b == nil {
			continue
		}
		if i.tick%every == 0 {
			i.act(p, b, b.brain.Decide(i.botView(p, b)))
		}
		b.seq++
		p.pending = enqueueInput(p.pending, inputMsg{id: id, seq: b.seq, in: b.keys, view: uint32(i.tick)}, p.lastSeq) //nolint:gosec // a match lasts minutes
	}
}

// botView is what the bot p sees now: itself, the match's ring, and the world's judgement of its follow-ups.
func (i *Instance) botView(p *player, b *bot) bots.View {
	var buffs []string
	for id := range p.buffs {
		buffs = append(buffs, id)
	}
	slices.Sort(buffs)
	return bots.View{
		Tick: i.tick, Fighting: i.fighting(), Ring: i.match.ring, Team: i.match.teams[i.match.fighters[p.ID].side],
		Self: bots.Self{
			ID: p.ID, Side: i.side(p.ID), State: p.state, HP: p.HP, MaxHP: p.derived.MaxHP, Energy: p.Energy, Down: p.dead || p.HP <= 0,
			Hard: p.statuses.Hard(), CantAct: p.statuses.Blocks(gamedata.BlocksAct), Silenced: p.statuses.Blocks(gamedata.BlocksSkills),
			SkillFrom: p.action.nextStart(skillAction), Waiting: p.waitingCast != nil, Attacking: p.attackQueued, Ready: p.skillReady, Buffs: buffs,
		},
		Holds: func(root, skill string, lock model.EntityRef) bool {
			f, ok := p.class.FollowUp(root, skill)
			return ok && i.holds(p, root, f.When, b.seq, lock)
		},
	}
}

// act takes the bot's decision as its client's input and presses: the keys it holds from now, its attack queued, and its skill use put with this tick's, pressed during its latest input.
func (i *Instance) act(p *player, b *bot, d bots.Decision) {
	b.keys = d.Keys
	if a := d.Attack; a != nil && !p.attackQueued {
		p.attackQueued, p.attackAim, p.attackTarget = true, a.Aim, a.Lock
	}
	if u := d.Skill; u != nil {
		i.casts = append(i.casts, skillMsg{id: p.ID, in: *u, seq: b.seq, done: make(chan error, 1)})
	}
}

// seenStarting keeps an action p starts in the match's ring for its bots, its next blow expected at its cancel point when nextAtCancel and else at its end; nothing in a match without them.
func (i *Instance) seenStarting(p *player, start, length uint64, t gamedata.Timing, nextAtCancel, blow bool, shape gamedata.Shape, lock model.EntityRef, mv gamedata.Move) {
	if i.match == nil || i.match.ring == nil {
		return
	}
	hitAt, cancelAt := model.ActionPoints(length, t.Hit, t.Cancel)
	s := bots.Started{By: p.ID, Shown: i.tick, Start: start, Hit: start + hitAt, Next: start + length, Blow: blow}
	if nextAtCancel {
		s.Next = start + cancelAt
	}
	if blow {
		s.Shape, s.Aim, s.Lock = shape, model.Aim{X: p.state.X, Y: p.state.Y, Z: p.state.Z, Yaw: float64(p.aim)}, lock
		if mv.Toward == gamedata.TowardLock && mv.Kind != gamedata.BlinkMove {
			s.Toward = mv.Distance
		}
	}
	i.match.ring.Start(s)
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
