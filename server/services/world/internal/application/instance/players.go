package instance

import (
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

// A backlog after a stall is cut to the newest few, so the player catches up.
const (
	inputQueueMax  = 4
	inputQueueKeep = 2
	inputHoldTicks = 5
)

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
	invulnUntil   uint64
	dead          bool
	deadUntil     uint64
	changed       bool          // HP, level or death changed this tick
	private       []model.Event // events for this player only
}

func (i *Instance) add(j *joinMsg) {
	if j.held != nil && !j.held() {
		j.done <- ErrMoved
		return
	}
	if _, dup := i.players[j.player.ID]; dup {
		i.remove(j.player.ID) // a stale presence; the new connection wins
	}
	class, ok := i.data.Class(j.player.Class)
	formulas, ok2 := i.data.Formulas(j.player.Class)
	if !ok || !ok2 {
		j.done <- model.ErrClassUnavailable
		return
	}
	p := &player{
		Player:  j.player,
		sink:    j.sink,
		state:   sim.State{X: j.player.X, Y: j.player.Y, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform},
		joined:  true,
		slot:    i.nextSlot,
		class:   class,
		derived: progression.DeriveStats(formulas, j.player.Stats, j.player.Level, progression.Equipment{}),
	}
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

func cutInputs(q []inputMsg) []inputMsg {
	if len(q) > inputQueueMax {
		return slices.Delete(q, 0, len(q)-inputQueueKeep)
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
		p.pending = cutInputs(p.pending)
		hold := false
		if len(p.pending) > 0 {
			next := p.pending[0]
			p.held = next.in
			p.lastSeq = next.seq
			p.pending = slices.Delete(p.pending, 0, 1)
			p.starved = 0
		} else {
			p.starved++
			hold = p.starved <= inputHoldTicks
		}
		before := p.state
		if !p.dead && !hold {
			for range i.subSteps {
				p.state = sim.Step(p.state, p.held, i.m.Geometry, i.params)
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
	cp := model.Checkpoint{ID: p.ID, MapID: mapID, X: p.state.X, Y: p.state.Y, HP: p.HP, MP: p.MP, Level: p.Level, Exp: p.Exp, Stats: p.Stats}
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
