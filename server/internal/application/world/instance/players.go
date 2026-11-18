package instance

import (
	"fmt"
	"slices"
	"time"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
	"finga/pkg/transient"
)

// A backlog of inputs past inputBacklog is cut to its newest inputKeep, so the player catches up; one that stays deeper than inputKeep for inputDrain loses its oldest input a tick till it's back, so a lead a hitch built doesn't last; a gap up to inputHold holds them still.
const (
	inputBacklog = 200 * time.Millisecond
	inputKeep    = 100 * time.Millisecond
	inputDrain   = time.Second
	inputHold    = 250 * time.Millisecond
)

// inputTicks are the input limits in the map's ticks.
type inputTicks struct {
	backlog, keep, drain, hold int
}

func inputTicksAt(interval time.Duration) inputTicks {
	n := func(d time.Duration) int { return int(model.TicksIn(d, interval)) }
	return inputTicks{backlog: n(inputBacklog), keep: n(inputKeep), drain: n(inputDrain), hold: n(inputHold)}
}

type player struct {
	model.Player
	sink    Sink
	state   sim.State
	held    sim.Input
	view    uint32     // the held input's
	pending []inputMsg // unapplied inputs, by sequence number
	lastSeq uint32
	starved int // ticks in a row with no queued input
	deep    int // ticks in a row its queue held more than inputKeep
	moved   bool
	joined  bool
	slot    uint64 // checkpointed on the ticks equal to it mod ticksPer

	class        gamedata.Class
	derived      progression.Derived
	attackQueued bool
	attackAim    float32         // the queued attack's
	attackTarget model.EntityRef // the queued attack's lock, zero for none
	aim          float32         // the yaw the latest action started aimed along
	action       action
	windUps      []windUp // hits still to land
	invulnUntil  uint64   // shown: a hit's blink
	nextHit      uint64   // a mob's attack hits again from this tick, after a hit or MISS
	moveMark     *moveMark
	dodgeFrom    uint64 // a skill's dodge runs from this tick until dodgeUntil
	dodgeUntil   uint64
	dodged       bool // a mob's attack has passed through a dodge, the latest in the frame acking dodgedSeq
	dodgedSeq    uint32
	blockFrom    uint64 // a block skill's guard runs from this tick until blockUntil
	blockUntil   uint64
	blockX       float64 // the way the guard faces: its aim at the press
	blockZ       float64
	blockGave    bool // this use's first blocked attack has given its energy
	blocked      bool // a block has stopped a mob's attack, the latest in the frame acking blockedSeq
	blockedSeq   uint32
	hitSeqs      map[string]uint64 // the input each skill's latest hit falls in, as its user's client counts from the press
	dead         bool
	deadUntil    uint64
	changed      bool                         // HP, level, death or aim changed this tick
	private      []model.Event                // events for this player only
	itemCalls    int                          // equips, unequips, hand-ins and potions in flight, which keep the player on this map
	handingIn    map[string]bool              // quests whose hand-in is in flight
	promoting    bool                         // a promotion's gift is in flight
	questChanges map[string]model.QuestChange // this tick's, told at its end
	toldHP       int                          // the HP and max HP the party last heard
	toldMaxHP    int
	hpCarry      int               // regen's fractions, in thousandths of a point
	toldEnergy   int               // the energy the client last heard, as of its ack; -1 until the first frame
	spentAhead   []energySpend     // spends for presses past lastSeq, which the client still takes off itself
	skillReady   map[string]uint64 // the tick each skill's cooldown ends
	waitingCast  *skillMsg         // a skill use waiting for its action's start
	channel      *channel          // a teleport under way
	buffs        map[string]buff   // by skill
	statuses     model.Statuses
	combatUntil  uint64      // in combat before this tick: combat's length after the latest Damage naming the player
	toMatch      *toMatchMsg // a hand-off to a match waiting for the item calls in flight
	bot          *brain      // a match's bot's, which presses its keys; nil for a player
}

func (i *Instance) add(j *joinMsg) {
	if _, dup := i.players[j.player.ID]; dup {
		i.remove(j.player.ID) // a stale presence; the new connection wins
	}
	if i.match != nil && !i.joinMatch(j) {
		j.done <- model.ErrNotInMatch
		return
	}
	class, ok := i.data.Class(j.player.Class)
	if !ok {
		j.done <- model.ErrClassUnavailable
		return
	}
	if j.player.Quests == nil {
		j.player.Quests = model.Quests{}
	}
	p := &player{
		Player: j.player,
		sink:   j.sink,
		state:  i.standing(j.player.X, j.player.Y, j.player.Z, j.player.Yaw),
		joined: true,
		slot:   i.nextSlot,
		class:  class,

		toldEnergy: -1,
		hitSeqs:    map[string]uint64{},
	}
	i.startTimers(p, time.Now())
	p.derived = i.derive(p)
	p.HP = min(p.HP, p.derived.MaxHP)
	p.Energy = min(max(p.Energy, 0), i.data.Energy().Max)
	i.nextSlot = (i.nextSlot + 1) % i.ticksPer // players joining together save on different ticks
	i.players[p.ID] = p
	i.order = append(i.order, p.ID)
	snap := model.Snapshot{Tick: i.tick, MapID: i.m.ID, Players: make([]model.PlayerView, 0, len(i.order)), Mobs: i.arena.Views(), Drops: i.ground.Views()}
	for _, id := range i.order {
		snap.Players = append(snap.Players, i.view(i.players[id]))
	}
	if i.boss != nil {
		up := i.bossUp()
		snap.FieldBoss = &up
	}
	if !j.sink.Send(snap) {
		i.remove(p.ID)
		j.done <- ErrSinkClosed
		return
	}
	if i.match != nil {
		p.bot = i.brainFor(p)
		i.welcome(p)
	}
	i.tellParty(p, i.m.ID) // a join is a map change, and gives the party the member as the world has them
	for _, h := range p.HandedIn {
		if i.handedIn(p, h) {
			i.log.Info("a hand-in the world missed finished at the join", "character_id", p.ID, "quest_id", h.Quest, "completion", h.Completion)
		}
	}
	p.HandedIn = nil
	j.done <- nil
}

// energySpend is the energy a skill spent, for a press made during input seq.
type energySpend struct {
	seq  uint32
	cost int
}

// spendAhead keeps a spend for a press during or past the last input stepped; spends for the same input add up, so a client's seqs can't grow the list.
func (p *player) spendAhead(seq uint32, cost int) {
	if seq < p.lastSeq {
		return
	}
	if k := slices.IndexFunc(p.spentAhead, func(s energySpend) bool { return s.seq == seq }); k >= 0 {
		p.spentAhead[k].cost += cost
		return
	}
	p.spentAhead = append(p.spentAhead, energySpend{seq: seq, cost: cost})
}

// energyAsOfAck is the energy as of the last input applied, as the client counts it. A press counts from the input after the one it came during: the world may take it in after that input steps, but the gateway posts no later input until the press is answered. So a spend for a press during or past the last input is added back, and a skill pressed before it and still waiting for its turn is taken off.
func (p *player) energyAsOfAck() int {
	p.spentAhead = slices.DeleteFunc(p.spentAhead, func(s energySpend) bool { return s.seq < p.lastSeq })
	e := p.Energy
	for _, s := range p.spentAhead {
		e += s.cost
	}
	if w := p.waitingCast; w != nil && w.seq < p.lastSeq {
		e -= w.cost
	}
	return max(e, 0)
}

func (i *Instance) remove(id kernel.CharacterID) {
	p, ok := i.players[id]
	if !ok {
		return
	}
	i.checkpoint(p)
	i.detach(id)
	i.leftMatch(id)
}

// detach drops a player without a checkpoint; the caller has saved one. A skill use still waiting for its turn hears it won't go.
func (i *Instance) detach(id kernel.CharacterID) {
	if m := i.players[id].waitingCast; m != nil {
		m.done <- fmt.Errorf("%w: the character left the map before its skill's turn", transient.ErrUnavailable)
	}
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x kernel.CharacterID) bool { return x == id })
	i.arena.DropBy(id) // their damage over time ends with them
}

func enqueueInput(q []inputMsg, in inputMsg, lastSeq uint32) []inputMsg {
	if in.seq <= lastSeq {
		return q
	}
	at, dup := slices.BinarySearchFunc(q, in.seq, func(m inputMsg, seq uint32) int { return int(m.seq) - int(seq) })
	if dup {
		return q
	}
	return slices.Insert(q, at, in)
}

func (t inputTicks) cut(q []inputMsg) []inputMsg {
	if len(q) > t.backlog {
		return slices.Delete(q, 0, len(q)-t.keep)
	}
	return q
}

// deepFor is how many ticks running q has stayed deeper than keep, from deep before this one.
func (t inputTicks) deepFor(q []inputMsg, deep int) int {
	if len(q) <= t.keep {
		return 0
	}
	return deep + 1
}

// simulate applies one queued input per player per tick; a short gap holds still to match the client's prediction.
func (i *Instance) simulate() (changed []kernel.CharacterID) {
	for _, id := range i.order {
		p := i.players[id]
		if p.invulnUntil == i.tick || p.dodgeUntil > 0 && (p.dodgeFrom == i.tick || p.dodgeUntil == i.tick) {
			p.changed = true // the Delta carries the invulnerability's end and the dodge's edges
		}
		p.pending = i.inputs.cut(p.pending)
		if p.deep = i.inputs.deepFor(p.pending, p.deep); p.deep > i.inputs.drain {
			p.pending = slices.Delete(p.pending, 0, 1) // deep a second: the oldest goes
		}
		hold := false
		i.restrain(p)
		if len(p.pending) > 0 {
			next := p.pending[0]
			p.held, p.view = next.in, next.view
			p.lastSeq = next.seq
			p.pending = slices.Delete(p.pending, 0, 1)
			p.starved = 0
			i.movedDuring(p, next.seq, next.in)
		} else {
			p.starved++
			if hold = p.starved <= i.inputs.hold; !hold {
				i.movedOn(p) // the held keys step on, the player moving in the channel
			}
		}
		before := p.state
		if !p.dead && !hold {
			moves := i.movement(p)
			g := i.geometry()
			g.Blockers = i.arena.BlockersAt(i.seen(p.view))
			if m := p.moveMark; m != nil && m.after < p.lastSeq {
				p.moveMark = nil
				if m.breaks { // from the press's next input, as its client predicts it; the hold's end sends the frame
					p.statuses.EndHard(i.tick)
					p.state.Held = 0
				}
				if m.kind != "" && p.state.Held == 0 { // a held body starts no move
					p.state = m.start(p.state, g, moves)
				}
			}
			for range i.subSteps {
				p.state = sim.Step(p.state, p.held, g, moves)
			}
			if i.fellOut(p.state) {
				i.rescue(p)
			}
		}
		p.moved = before != p.state
		if p.moved || p.joined {
			changed = append(changed, id)
		}
	}
	return changed
}

// seen bounds an input's view to the frames kept, so a made-up one can't slip past a mob.
func (i *Instance) seen(view uint32) uint64 {
	if i.tick == 0 {
		return 0
	}
	return min(max(uint64(view), i.tick-min(i.tick, mobs.History)), i.tick-1)
}

// living are the players alive on the map, whom mobs stop at and attack.
func (i *Instance) living() []mobs.Body {
	bodies := make([]mobs.Body, 0, len(i.order))
	for _, id := range i.order {
		if p := i.players[id]; !p.dead {
			bodies = append(bodies, mobs.Body{ID: p.ID, Body: i.bodyOf(p)})
		}
	}
	return bodies
}

// seqAhead is how far past the newest input received a press may say it came: its input comes by another call and may land a moment after it.
const seqAhead = 2

// pressedDuring holds the client's seq for a press between the last input stepped and just past the newest received, so a made-up one can't put off what waits on it.
func (p *player) pressedDuring(seq uint32) uint32 {
	newest := p.lastSeq
	if n := len(p.pending); n > 0 {
		newest = p.pending[n-1].seq
	}
	return min(max(seq, p.lastSeq), newest+seqAhead)
}

// aimAt turns the player's view to an action's aim, telling the map when it turned.
func (p *player) aimAt(yaw float32) {
	if p.aim != yaw {
		p.aim, p.changed = yaw, true
	}
}

func (i *Instance) view(p *player) model.PlayerView {
	return model.PlayerView{
		ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.derived.MaxHP, Dead: p.dead, Invulnerable: p.invulnUntil > i.tick,
		Dodging: p.dodging(i.tick), Shield: p.shield(), State: p.state, Aim: p.aim,
		Statuses: p.statuses.Shown(), Team: i.side(p.ID), Bot: p.bot != nil,
	}
}

// checkpoint never blocks: saves keeps each character's newest checkpoint until it is saved.
func (i *Instance) checkpoint(p *player) { i.checkpointOn(p, i.m.ID) }

func (i *Instance) checkpointOn(p *player, mapID string) {
	if p.bot != nil {
		return // a bot has no row
	}
	if b := p.Back; b != nil { // in a match, where they go back to
		c := b.Player
		i.saves.Put(model.Checkpoint{ID: c.ID, Class: c.Class, MapID: b.MapID, X: c.X, Y: c.Y, Z: c.Z, Yaw: c.Yaw, HP: c.HP, Energy: c.Energy, Level: c.Level, Exp: c.Exp, Points: c.Points, Skills: c.Skills.Clone(), Quests: c.Quests.Clone(), Timers: c.Timers.Clone()})
		return
	}
	cp := model.Checkpoint{ID: p.ID, Class: p.Class, MapID: mapID, X: p.state.X, Y: p.state.Y, Z: p.state.Z, Yaw: facing(p.state), HP: p.HP, Energy: p.Energy, Level: p.Level, Exp: p.Exp, Points: p.Points, Skills: p.Skills.Clone(), Quests: p.Quests.Clone(), Timers: i.timersOf(p, time.Now())}
	if p.dead { // the respawn it is owed; the death penalty is already taken
		town := i.town()
		at := standOn(town, i.params, town.PlayerSpawn.X, 0, town.PlayerSpawn.Z, float32(town.SpawnYaw))
		cp.MapID, cp.X, cp.Y, cp.Z, cp.Yaw, cp.HP = town.ID, at.X, at.Y, at.Z, float32(town.SpawnYaw), RespawnHP(p.derived.MaxHP)
	}
	i.saves.Put(cp)
}

// town is where the map's dead respawn; loading checks that every map names one.
func (i *Instance) town() *gamedata.Map {
	town, _ := i.data.Map(i.m.Town)
	return town
}
