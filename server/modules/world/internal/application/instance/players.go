package instance

import (
	"fmt"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/modules/world/internal/application/mobs"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
)

// A backlog of inputs past inputBacklog is cut to its newest inputKeep, so the player catches up; a gap up to inputHold holds them still.
const (
	inputBacklog = 200 * time.Millisecond
	inputKeep    = 100 * time.Millisecond
	inputHold    = 250 * time.Millisecond
)

// inputTicks are the input limits in the map's ticks.
type inputTicks struct {
	backlog, keep, hold int
}

func inputTicksAt(interval time.Duration) inputTicks {
	n := func(d time.Duration) int { return int(model.TicksIn(d, interval)) }
	return inputTicks{backlog: n(inputBacklog), keep: n(inputKeep), hold: n(inputHold)}
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
	moved   bool
	joined  bool
	slot    uint64 // checkpointed on the ticks equal to it mod ticksPer

	class        gamedata.Class
	derived      progression.Derived
	attackQueued bool
	attackSeq    uint32 // the input the queued attack was pressed during
	castMarks    []castMark
	action       action
	windUps      []windUp // hits still to land
	invulnUntil  uint64   // shown: a hit's blink
	nextHit      uint64   // a mob's attack hits again from this tick, after a hit or MISS
	dead         bool
	deadUntil    uint64
	changed      bool                         // HP, level or death changed this tick
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
	buffs        map[string]buff   // by skill
}

func (i *Instance) add(j *joinMsg) {
	if _, dup := i.players[j.player.ID]; dup {
		i.remove(j.player.ID) // a stale presence; the new connection wins
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
		state:  standing(j.player.X, j.player.Y),
		joined: true,
		slot:   i.nextSlot,
		class:  class,

		toldEnergy: -1,
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

// energyAsOfAck is the energy as of the last input applied, as the client counts it. A press counts from the input after the one it came during, as its casting does: the world may take it in after that input steps, but the gateway posts no later input until the press is answered. So a spend for a press during or past the last input is added back, and a skill pressed before it and still waiting for its turn is taken off.
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
}

// detach drops a player without a checkpoint; the caller has saved one. A skill use still waiting for its turn hears it won't go.
func (i *Instance) detach(id kernel.CharacterID) {
	if m := i.players[id].waitingCast; m != nil {
		m.done <- fmt.Errorf("%w: the character left the map before its skill's turn", transient.ErrUnavailable)
	}
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x kernel.CharacterID) bool { return x == id })
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

// simulate applies one queued input per player per tick; a short gap holds still to match the client's prediction.
func (i *Instance) simulate() (changed []kernel.CharacterID) {
	for _, id := range i.order {
		p := i.players[id]
		if p.invulnUntil == i.tick {
			p.changed = true // the Delta carries the invulnerability's end
		}
		p.pending = i.inputs.cut(p.pending)
		hold := false
		if len(p.pending) > 0 {
			next := p.pending[0]
			p.held, p.view = next.in, next.view
			p.lastSeq = next.seq
			p.pending = slices.Delete(p.pending, 0, 1)
			p.markCasting()
			p.starved = 0
		} else {
			p.starved++
			hold = p.starved <= i.inputs.hold
		}
		before := p.state
		if !p.dead && !hold {
			moves := i.movement(p)
			g := i.m.Geometry
			g.Blockers = i.arena.BoxesAt(i.seen(p.view))
			for range i.subSteps {
				p.state = sim.Step(p.state, p.held, g, moves)
			}
			if fellOut(p.state, i.m) {
				i.rescue(p)
			}
		}
		p.moved = movedFrom(before, p.state)
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
			bodies = append(bodies, mobs.Body{ID: p.ID, Box: i.playerBox(p)})
		}
	}
	return bodies
}

// castMark sets the casting before the first input stepped past an input: a skill's length in sub-steps as it starts, or 0 as an attack starts.
type castMark struct {
	after   uint32
	casting int
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

// mark sets the casting from the first input stepped past the one a press came during. A later mark for the same input replaces an earlier one, so the marks stay few.
func (p *player) mark(seq uint32, casting int) {
	after := p.pressedDuring(seq)
	p.castMarks = slices.DeleteFunc(p.castMarks, func(m castMark) bool { return m.after == after })
	p.castMarks = append(p.castMarks, castMark{after: after, casting: casting})
}

// markCasting applies, in order, the marks the input just taken is past; one the server took late applies at once, an input later than the client's.
func (p *player) markCasting() {
	p.castMarks = slices.DeleteFunc(p.castMarks, func(m castMark) bool {
		if m.after >= p.lastSeq {
			return false
		}
		p.state.Casting = m.casting
		return true
	})
}

// movedFrom is whether anything drawn changed: casting alone doesn't, and the player's own client predicts it.
func movedFrom(before, after sim.State) bool {
	before.Casting, after.Casting = 0, 0
	return before != after
}

func (i *Instance) view(p *player) model.PlayerView {
	return model.PlayerView{ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.derived.MaxHP, Dead: p.dead, Invulnerable: p.invulnUntil > i.tick, State: p.state}
}

func (i *Instance) playerBox(p *player) model.Rect {
	return model.Rect{X: p.state.X - i.params.Width/2, Y: p.state.Y - i.params.Height, W: i.params.Width, H: i.params.Height}
}

// checkpoint never blocks: saves keeps each character's newest checkpoint until it is saved.
func (i *Instance) checkpoint(p *player) { i.checkpointOn(p, i.m.ID) }

func (i *Instance) checkpointOn(p *player, mapID string) {
	cp := model.Checkpoint{ID: p.ID, Class: p.Class, MapID: mapID, X: p.state.X, Y: p.state.Y, HP: p.HP, Energy: p.Energy, Level: p.Level, Exp: p.Exp, Points: p.Points, Skills: p.Skills.Clone(), Quests: p.Quests.Clone(), Timers: i.timersOf(p, time.Now())}
	if p.dead { // the respawn it is owed; the death penalty is already taken
		town := i.town()
		cp.MapID, cp.X, cp.Y, cp.HP = town.ID, town.PlayerSpawn.X, town.PlayerSpawn.Y, RespawnHP(p.derived.MaxHP)
	}
	i.saves.Put(cp)
}

// town is where the map's dead respawn; loading checks that every map names one.
func (i *Instance) town() *gamedata.Map {
	town, _ := i.data.Map(i.m.Town)
	return town
}
