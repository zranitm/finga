package instance

import (
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/model"
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
	pending []inputMsg // unapplied inputs, by sequence number
	lastSeq uint32
	starved int // ticks in a row with no queued input
	moved   bool
	joined  bool
	slot    uint64 // checkpointed on the ticks equal to it mod ticksPer

	class         gamedata.Class
	derived       progression.Derived
	attackQueued  bool
	cooldownUntil uint64
	invulnUntil   uint64 // shown: a hit's blink
	nextTouch     uint64 // a mob's touch is rolled again from this tick, hit or MISS
	dead          bool
	deadUntil     uint64
	changed       bool                         // HP, level or death changed this tick
	private       []model.Event                // events for this player only
	itemCalls     int                          // equips, unequips, hand-ins and potions in flight, which keep the player on this map
	handingIn     map[string]bool              // quests whose hand-in is in flight
	questChanges  map[string]model.QuestChange // this tick's, told at its end
	toldHP        int                          // the HP and max HP the party last heard
	toldMaxHP     int
	hpCarry       int // regen's fractions, in thousandths of a point
	mpCarry       int
	skillReady    map[string]uint64 // the tick each skill's cooldown ends
	buffs         map[string]buff   // by skill
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
	}
	p.derived = i.derive(p)
	p.HP = min(p.HP, p.derived.MaxHP)
	p.MP = min(p.MP, p.derived.MaxMP)
	i.nextSlot = (i.nextSlot + 1) % i.ticksPer // players joining together save on different ticks
	i.players[p.ID] = p
	i.order = append(i.order, p.ID)
	snap := model.Snapshot{Tick: i.tick, MapID: i.m.ID, Players: make([]model.PlayerView, 0, len(i.order)), Mobs: i.arena.Views(), Drops: i.ground.Views()}
	for _, id := range i.order {
		snap.Players = append(snap.Players, i.view(i.players[id]))
	}
	if !j.sink.Send(snap) {
		i.remove(p.ID)
		j.done <- ErrSinkClosed
		return
	}
	i.tellParty(p, i.m.ID) // a join is a map change, and after a restart it corrects what social kept
	for _, h := range p.HandedIn {
		if i.handedIn(p, h) {
			i.log.Info("a hand-in the world missed finished at the join", "character_id", p.ID, "quest_id", h.Quest, "completion", h.Completion)
		}
	}
	p.HandedIn = nil
	j.done <- nil
}

func (i *Instance) remove(id kernel.CharacterID) {
	p, ok := i.players[id]
	if !ok {
		return
	}
	i.checkpoint(p)
	i.detach(id)
}

// detach drops a player without a checkpoint; the caller has saved one.
func (i *Instance) detach(id kernel.CharacterID) {
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
			p.held = next.in
			p.lastSeq = next.seq
			p.pending = slices.Delete(p.pending, 0, 1)
			p.starved = 0
		} else {
			p.starved++
			hold = p.starved <= i.inputs.hold
		}
		before := p.state
		if !p.dead && !hold {
			moves := i.movement(p)
			for range i.subSteps {
				p.state = sim.Step(p.state, p.held, i.m.Geometry, moves)
			}
			if fellOut(p.state, i.m) {
				i.rescue(p)
			}
		}
		p.moved = p.state != before
		if p.moved || p.joined {
			changed = append(changed, id)
		}
	}
	return changed
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
	cp := model.Checkpoint{ID: p.ID, MapID: mapID, X: p.state.X, Y: p.state.Y, HP: p.HP, MP: p.MP, Level: p.Level, Exp: p.Exp, Points: p.Points, Skills: p.Skills.Clone(), Quests: p.Quests.Clone()}
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
