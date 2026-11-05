package application

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/tick"
	"finga/services/world/internal/model"
)

const (
	drainLimit = 1024 // inbox messages handled per tick; the rest wait
	inboxSize  = 4096

	respawnDelay     = 3 * time.Second
	respawnHPPercent = 50
	invulnerability  = time.Second // after contact damage
	mobPause         = time.Second // at a platform edge
	knockbackX       = 16.0        // px away from the mob
	knockbackVY      = -250.0
)

// Tuning is how often the instances tick and checkpoint.
type Tuning struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	FlushTimeout       time.Duration // the World's last saves on shutdown, all together
}

// Deps are the services every map instance calls; all are required.
type Deps struct {
	NewRoll  func() model.Roll // each instance's random source, used only on its tick goroutine
	Observer TickObserver
	Items    Pickups
	Workers  *Workers
	Parties  Parties
}

// Instance is one map's actor; only its tick goroutine touches its state.
type Instance struct {
	m        *gamedata.Map
	data     *gamedata.Data
	params   sim.Params
	subSteps int // sim steps per tick
	tuning   Tuning
	deps     Deps
	mover    Mover
	roll     model.Roll
	inbox    chan message
	persist  chan<- model.Job

	tick     uint64
	players  map[kernel.CharacterID]*player
	order    []kernel.CharacterID // join order, so iteration is deterministic
	mobs     []*mob               // spawn order; removed on death
	spawns   []spawnState
	nextMob  uint64
	drops    []*drop
	nextDrop uint64
	ticksPer uint64 // ticks between a player's checkpoints
	nextSlot uint64 // the sweep tick, mod ticksPer, the next player to join gets

	dropped      []kernel.CharacterID // removed during the last broadcast, reported next tick
	removedMobs  []uint64
	newDrops     []model.DropView
	removedDrops []uint64
	events       []model.Event // map-wide events of the current tick
}

// message is what the tick drains from the inbox.
//
//sumtype:decl
type message interface{ message() }

// leaveMsg removes a player, only if it joined through conn (empty: any).
type leaveMsg struct {
	id   kernel.CharacterID
	conn string
}

type (
	attackMsg struct{ id kernel.CharacterID }
	pickMsg   struct{ id kernel.CharacterID }
	portalMsg struct{ id kernel.CharacterID }
)

func (*joinMsg) message()      {}
func (leaveMsg) message()      {}
func (inputMsg) message()      {}
func (attackMsg) message()     {}
func (*pickupResult) message() {}
func (pickMsg) message()       {}
func (portalMsg) message()     {}
func (chatMsg) message()       {}

type joinMsg struct {
	player   model.Player
	sink     Sink
	done     chan error
	transfer bool // a transfer never replaces the character's presence through another connection
}

// errReplaced is a transfer that found its character held through a newer connection.
var errReplaced = errors.New("a newer connection holds the character")

type inputMsg struct {
	id  kernel.CharacterID
	seq uint32
	in  sim.Input
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
	invulnUntil   uint64
	dead          bool
	deadUntil     uint64
	changed       bool          // HP, level or death changed this tick
	private       []model.Event // events for this player only
}

// NewInstance builds the actor for m; its checkpoints and level-ups go to persist, and mover takes its
// players to other maps. Run starts it.
func NewInstance(m *gamedata.Map, data *gamedata.Data, persist chan<- model.Job, mover Mover, t Tuning, d Deps) *Instance {
	ticksPer := uint64(t.CheckpointInterval / t.TickInterval)
	if ticksPer == 0 {
		ticksPer = 1
	}
	params := data.Movement()
	return &Instance{
		m: m, data: data, params: params, tuning: t, deps: d, mover: mover, roll: d.NewRoll(),
		subSteps: max(1, int(math.Round(t.TickInterval.Seconds()/params.Dt))),
		inbox:    make(chan message, inboxSize),
		persist:  persist,
		players:  map[kernel.CharacterID]*player{},
		spawns:   make([]spawnState, len(m.Spawns)),
		ticksPer: ticksPer,
	}
}

// Run ticks until ctx is done, then checkpoints the players still on the map.
func (i *Instance) Run(ctx context.Context) error {
	mapID := i.m.ID
	err := tick.Run(ctx, i.tuning.TickInterval, i, tick.WithObserver(func(_ uint64, took, interval time.Duration) {
		i.deps.Observer.ObserveTick(mapID, took, interval)
	}))
	for _, id := range i.order {
		i.checkpoint(i.players[id])
	}
	return err
}

// Join adds a player and waits until its Snapshot is sent; ErrBusy when the inbox is full.
func (i *Instance) Join(ctx context.Context, p model.Player, sink Sink) error {
	return i.join(ctx, &joinMsg{player: p, sink: sink})
}

func (i *Instance) join(ctx context.Context, j *joinMsg) error {
	done := make(chan error, 1)
	j.done = done
	if !i.post(j) {
		return model.ErrBusy
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LeaveConnection removes a player only if it joined through conn, so a stale Leave is a no-op.
func (i *Instance) LeaveConnection(id kernel.CharacterID, conn string) bool {
	return i.post(leaveMsg{id: id, conn: conn})
}

// Input queues held keys without blocking; a dropped input is superseded by the next.
func (i *Instance) Input(id kernel.CharacterID, seq uint32, in sim.Input) bool {
	return i.post(inputMsg{id: id, seq: seq, in: in})
}

// Attack queues a basic attack; the tick enforces the cooldown.
func (i *Instance) Attack(id kernel.CharacterID) bool {
	return i.post(attackMsg{id: id})
}

// PickUp asks for the nearest eligible drop within reach.
func (i *Instance) PickUp(id kernel.CharacterID) bool {
	return i.post(pickMsg{id: id})
}

// EnterPortal takes the portal the character stands in, if any.
func (i *Instance) EnterPortal(id kernel.CharacterID) bool {
	return i.post(portalMsg{id: id})
}

func (i *Instance) post(m message) bool {
	select {
	case i.inbox <- m:
		return true
	default:
		return false
	}
}

// Step runs one tick. Nothing here may block or touch the database.
func (i *Instance) Step(n uint64, _ time.Duration) {
	i.tick = n
	i.events = i.events[:0]
	i.removedMobs = i.removedMobs[:0]
	i.newDrops = i.newDrops[:0]
	i.removedDrops = i.removedDrops[:0]
	removed := slices.Concat(i.dropped, i.drain())
	changed := i.simulate()
	i.stepMobs()
	i.resolveAttacks()
	i.contactDamage()
	removed = append(removed, i.deathsAndRespawns()...)
	i.expireDrops()
	for _, id := range i.order {
		if p := i.players[id]; p.slot == n%i.ticksPer {
			i.checkpoint(p)
		}
	}
	i.dropped = i.broadcast(changed, i.gone(removed))
}

// gone keeps the removed players who are not back: a Leave and a rejoin in one tick is an update, and the
// client applies removals after updates.
func (i *Instance) gone(removed []kernel.CharacterID) []kernel.CharacterID {
	out := removed[:0]
	for _, id := range removed {
		if _, back := i.players[id]; !back && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func (i *Instance) drain() []kernel.CharacterID {
	var removed []kernel.CharacterID
	for range drainLimit {
		var m message
		select {
		case m = <-i.inbox:
		default:
			return removed
		}
		switch m := m.(type) {
		case *joinMsg:
			i.add(m)
		case inputMsg:
			if p, ok := i.players[m.id]; ok {
				p.pending = enqueueInput(p.pending, m, p.lastSeq)
			}
		case attackMsg:
			if p, ok := i.players[m.id]; ok {
				p.attackQueued = true
			}
		case *pickupResult:
			i.finishPickup(m)
		case pickMsg:
			if p, ok := i.players[m.id]; ok {
				i.pickUp(p)
			}
		case portalMsg:
			if p, ok := i.players[m.id]; ok && i.enterPortal(p) {
				removed = append(removed, m.id)
			}
		case chatMsg:
			if _, ok := i.players[m.from]; ok {
				i.events = append(i.events, model.Chat{Tick: i.tick, Sender: m.from, SenderName: m.name, Text: m.text})
			}
		case leaveMsg:
			if p, ok := i.players[m.id]; ok && m.conn != "" && p.Connection != m.conn {
				continue // a newer connection holds the character now
			}
			if i.remove(m.id) {
				removed = append(removed, m.id)
			}
		}
	}
	return removed
}

func (i *Instance) add(j *joinMsg) {
	if old, dup := i.players[j.player.ID]; dup {
		if j.transfer && old.Connection != j.player.Connection {
			j.done <- errReplaced
			return
		}
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
	snap := model.Snapshot{Tick: i.tick, MapID: i.m.ID, Players: make([]model.PlayerView, 0, len(i.order)), Mobs: make([]model.MobView, 0, len(i.mobs)), Drops: i.dropViews()}
	for _, id := range i.order {
		snap.Players = append(snap.Players, i.view(i.players[id]))
	}
	for _, mb := range i.mobs {
		snap.Mobs = append(snap.Mobs, mb.view())
	}
	if !j.sink.Send(snap) {
		i.remove(p.ID)
		j.done <- model.ErrBusy
		return
	}
	j.done <- nil
}

func (i *Instance) remove(id kernel.CharacterID) bool {
	p, ok := i.players[id]
	if !ok {
		return false
	}
	i.checkpoint(p)
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x kernel.CharacterID) bool { return x == id })
	return true
}

// A backlog after a stall is cut to the newest few, so the player catches up.
const (
	inputQueueMax  = 4
	inputQueueKeep = 2
	inputHoldTicks = 5
)

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

// simulate applies one queued input per player per tick. With none queued the player holds
// still for inputHoldTicks, so each seq matches the client's prediction, then the held keys run.
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

func (i *Instance) broadcast(changed, removed []kernel.CharacterID) []kernel.CharacterID {
	for _, id := range i.order {
		if p := i.players[id]; (p.changed || p.moved) && !slices.Contains(changed, id) {
			changed = append(changed, id)
		}
	}
	views := make([]model.PlayerView, 0, len(changed))
	for _, id := range changed {
		if p, ok := i.players[id]; ok {
			views = append(views, i.view(p))
		}
	}
	var mobViews []model.MobView
	for _, mb := range i.mobs {
		if mb.changed {
			mobViews = append(mobViews, mb.view())
			mb.changed = false
		}
	}
	frame := &model.Frame{Tick: i.tick, Players: views, Mobs: mobViews, Drops: slices.Clone(i.newDrops), Removed: removed, RemovedMobs: slices.Clone(i.removedMobs), RemovedDrops: slices.Clone(i.removedDrops)}
	var dropped []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		ok := p.sink.Send(model.Delta{Frame: frame, AckSeq: p.lastSeq})
		for _, e := range i.events {
			ok = ok && p.sink.Send(e)
		}
		for _, e := range p.private {
			ok = ok && p.sink.Send(e)
		}
		p.private = p.private[:0]
		p.joined = false
		p.changed = false
		if !ok {
			i.remove(id)
			dropped = append(dropped, id)
		}
	}
	return dropped
}

func (i *Instance) view(p *player) model.PlayerView {
	return model.PlayerView{ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.derived.MaxHP, Dead: p.dead, Invulnerable: p.invulnUntil > i.tick, State: p.state}
}

// checkpoint never blocks: a full queue drops it and the next sweep retries.
func (i *Instance) checkpoint(p *player) { i.checkpointOn(p, i.m.ID) }

func (i *Instance) ticksFor(d time.Duration) uint64 {
	n := uint64((d + i.tuning.TickInterval - 1) / i.tuning.TickInterval)
	if n == 0 {
		return 1
	}
	return n
}

func (i *Instance) playerBox(p *player) model.Rect {
	return model.Rect{X: p.state.X - i.params.Width/2, Y: p.state.Y - i.params.Height, W: i.params.Width, H: i.params.Height}
}

func attackerOf(d progression.Derived) model.Attacker {
	return model.Attacker{AttackPower: d.AttackPower, Accuracy: d.Accuracy, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration}
}

func defenderOf(d progression.Derived) model.Defender {
	return model.Defender{Defense: d.Defense, Evasion: d.Evasion}
}

// detach drops a player without a checkpoint; the caller has saved one.
func (i *Instance) detach(id kernel.CharacterID) {
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x kernel.CharacterID) bool { return x == id })
}

func (i *Instance) checkpointOn(p *player, mapID string) {
	cp := model.Checkpoint{ID: p.ID, MapID: mapID, X: p.state.X, Y: p.state.Y, HP: p.HP, MP: p.MP, Level: p.Level, Exp: p.Exp, Stats: p.Stats}
	select {
	case i.persist <- cp:
	default:
	}
}
