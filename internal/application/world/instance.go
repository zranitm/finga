package world

import (
	"context"
	"math/rand/v2"
	"slices"
	"time"

	"finga/internal/application/character"
	"finga/internal/application/combat"
	"finga/internal/application/gamedata"
	"finga/internal/application/world/sim"
	"finga/internal/model"
	"finga/pkg/tick"
)

// Defaults for Options.
const (
	DefaultTickInterval       = 50 * time.Millisecond
	DefaultSubSteps           = 3
	DefaultDrainLimit         = 1024
	DefaultInboxSize          = 4096
	DefaultCheckpointInterval = 30 * time.Second

	respawnDelay     = 3 * time.Second // page 5: respawn after 3 s
	respawnHPPercent = 50
	invulnerability  = time.Second // after contact damage
	knockbackX       = 16.0        // px away from the mob
	knockbackVY      = -250.0      // a small hop
)

// Options tune the instances. Zero values take the defaults.
type Options struct {
	TickInterval       time.Duration
	SubSteps           int // movement sub-steps per tick (page 5: 3 of 1/60 s)
	DrainLimit         int // inbox messages handled per tick; the rest wait
	InboxSize          int
	CheckpointInterval time.Duration // TD-02
	Observer           TickObserver  // nil: no metrics
	Seed               uint64        // the random seed; 0 picks one
	Roll               combat.Roll   // overrides the seeded generator, for tests
	Transfer           Mover         // moves players between maps; nil respawns in place
	Items              Pickups       // the item module; nil: pickups are unavailable
	Workers            *Workers      // the economy worker pool; nil: pickups are unavailable
	Parties            Parties       // the party service; nil: nobody has a party
}

func (o Options) withDefaults() Options {
	if o.TickInterval <= 0 {
		o.TickInterval = DefaultTickInterval
	}
	if o.SubSteps <= 0 {
		o.SubSteps = DefaultSubSteps
	}
	if o.DrainLimit <= 0 {
		o.DrainLimit = DefaultDrainLimit
	}
	if o.InboxSize <= 0 {
		o.InboxSize = DefaultInboxSize
	}
	if o.CheckpointInterval <= 0 {
		o.CheckpointInterval = DefaultCheckpointInterval
	}
	if o.Seed == 0 {
		o.Seed = rand.Uint64() //nolint:gosec // a game seed, recorded for replays
	}
	return o
}

// Instance is one map's actor. Everything inside it runs on its tick
// goroutine; other goroutines only send to its inbox.
type Instance struct {
	m       *gamedata.Map
	data    *gamedata.Data
	params  sim.Params
	opts    Options
	roll    combat.Roll
	inbox   chan message
	persist chan<- Job

	tick     uint64
	players  map[model.CharacterID]*player
	order    []model.CharacterID // join order, so iteration is deterministic
	mobs     []*mob              // spawn order; removed on death
	spawns   []spawnState
	nextMob  uint64
	drops    []*drop
	nextDrop uint64
	ticksPer uint64 // ticks between checkpoint sweeps

	dropped      []model.CharacterID // removed during the last broadcast, reported next tick
	removedMobs  []uint64
	newDrops     []DropView
	removedDrops []uint64
	events       []Event // map-wide events of the current tick
}

// message is one inbox item.
type message struct {
	join   *joinMsg
	leave  model.CharacterID
	input  *inputMsg
	attack model.CharacterID
	pickup *pickupResult
	pick   model.CharacterID
	portal model.CharacterID // EnterPortal
	chat   *chatMsg
}

type joinMsg struct {
	player Player
	sink   Sink
	done   chan error
}

type inputMsg struct {
	id  model.CharacterID
	seq uint32
	in  sim.Input
}

// player is an instance's view of a character: identity, the movement
// state, the input queue, and combat state.
type player struct {
	Player
	sink    Sink
	state   sim.State
	held    sim.Input
	pending []inputMsg // inputs not yet applied, by sequence number; one per tick
	lastSeq uint32
	moved   bool
	joined  bool

	class         gamedata.Class
	derived       character.Derived
	attackQueued  bool
	cooldownUntil uint64
	invulnUntil   uint64
	dead          bool
	deadUntil     uint64
	changed       bool    // HP, level or death changed this tick
	private       []Event // events for this player only
}

// NewInstance builds the actor for m. Run starts it.
func NewInstance(m *gamedata.Map, data *gamedata.Data, persist chan<- Job, opts Options) *Instance {
	opts = opts.withDefaults()
	ticksPer := uint64(opts.CheckpointInterval / opts.TickInterval)
	if ticksPer == 0 {
		ticksPer = 1
	}
	roll := opts.Roll
	if roll == nil {
		roll = rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9E3779B97F4A7C15)) //nolint:gosec // game randomness, seeded for replays
	}
	return &Instance{
		m: m, data: data, params: data.Movement(), opts: opts, roll: roll,
		inbox:    make(chan message, opts.InboxSize),
		persist:  persist,
		players:  map[model.CharacterID]*player{},
		spawns:   make([]spawnState, len(m.Spawns)),
		ticksPer: ticksPer,
	}
}

// MapID is the map this instance runs.
func (i *Instance) MapID() string { return i.m.ID }

// Seed is the random seed this instance runs with, for replaying a tick.
func (i *Instance) Seed() uint64 { return i.opts.Seed }

// Run ticks until ctx is done. Players still on the map are checkpointed
// on the way out.
func (i *Instance) Run(ctx context.Context) error {
	var opts []tick.Option
	if i.opts.Observer != nil {
		mapID := i.m.ID
		opts = append(opts, tick.WithObserver(func(_ uint64, took, interval time.Duration) {
			i.opts.Observer.ObserveTick(mapID, took, interval)
		}))
	}
	err := tick.Run(ctx, i.opts.TickInterval, i, opts...)
	for _, id := range i.order {
		i.checkpoint(i.players[id])
	}
	return err
}

// Join adds a player. It returns once the instance has accepted the join
// and sent the Snapshot, or ErrBusy when the inbox is full.
func (i *Instance) Join(ctx context.Context, p Player, sink Sink) error {
	done := make(chan error, 1)
	if !i.post(message{join: &joinMsg{player: p, sink: sink, done: done}}) {
		return ErrBusy
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Leave removes a player; its checkpoint is saved. Unknown IDs are ignored.
func (i *Instance) Leave(id model.CharacterID) bool {
	return i.post(message{leave: id})
}

// Input queues held keys with their sequence number. It never blocks: a
// full inbox drops the input, and the client's next one supersedes it.
func (i *Instance) Input(id model.CharacterID, seq uint32, in sim.Input) bool {
	return i.post(message{input: &inputMsg{id: id, seq: seq, in: in}})
}

// Attack queues a basic attack the way the character faces. The cooldown
// is enforced in the tick, so spamming it changes nothing.
func (i *Instance) Attack(id model.CharacterID) bool {
	return i.post(message{attack: id})
}

// PickUp asks for the nearest eligible drop within reach.
func (i *Instance) PickUp(id model.CharacterID) bool {
	return i.post(message{pick: id})
}

// EnterPortal takes the portal the character stands in, if any. The
// handoff to the target map happens inside the tick.
func (i *Instance) EnterPortal(id model.CharacterID) bool {
	return i.post(message{portal: id})
}

func (i *Instance) post(m message) bool {
	select {
	case i.inbox <- m:
		return true
	default:
		return false
	}
}

// Step is one tick (page 3): drain the inbox, apply inputs and simulate,
// run the mobs, resolve attacks, apply deaths and respawns, then send each
// player its delta and the tick's events. Nothing here blocks or touches
// the database.
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
	if n%i.ticksPer == 0 {
		for _, id := range i.order {
			i.checkpoint(i.players[id])
		}
	}
	i.dropped = i.broadcast(changed, removed)
}

// drain handles up to DrainLimit inbox messages; the rest wait for the
// next tick. It returns the players that left.
func (i *Instance) drain() []model.CharacterID {
	var removed []model.CharacterID
	for handled := 0; handled < i.opts.DrainLimit; handled++ {
		var m message
		select {
		case m = <-i.inbox:
		default:
			return removed
		}
		switch {
		case m.join != nil:
			i.join(m.join)
		case m.input != nil:
			if p, ok := i.players[m.input.id]; ok {
				p.pending = enqueueInput(p.pending, *m.input, p.lastSeq)
			}
		case !m.attack.IsZero():
			if p, ok := i.players[m.attack]; ok {
				p.attackQueued = true
			}
		case m.pickup != nil:
			i.finishPickup(m.pickup)
		case !m.pick.IsZero():
			if p, ok := i.players[m.pick]; ok {
				i.pickUp(p)
			}
		case !m.portal.IsZero():
			if p, ok := i.players[m.portal]; ok && i.enterPortal(p) {
				removed = append(removed, m.portal)
			}
		case m.chat != nil:
			if _, ok := i.players[m.chat.from]; ok {
				i.events = append(i.events, Chat{Tick: i.tick, Sender: m.chat.from, SenderName: m.chat.name, Text: m.chat.text})
			}
		case !m.leave.IsZero():
			if i.remove(m.leave) {
				removed = append(removed, m.leave)
			}
		}
	}
	return removed
}

func (i *Instance) join(j *joinMsg) {
	if _, dup := i.players[j.player.ID]; dup {
		i.remove(j.player.ID) // a stale presence; the new connection wins
	}
	class, ok := i.data.Class(j.player.Class)
	formulas, ok2 := i.data.Formulas(j.player.Class)
	if !ok || !ok2 {
		j.done <- character.ErrClassUnavailable
		return
	}
	p := &player{
		Player:  j.player,
		sink:    j.sink,
		state:   sim.State{X: j.player.X, Y: j.player.Y, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform},
		joined:  true,
		class:   class,
		derived: character.DeriveStats(formulas, j.player.Stats, j.player.Level, character.Equipment{}),
	}
	p.HP = min(p.HP, p.derived.MaxHP)
	p.MP = min(p.MP, p.derived.MaxMP)
	i.players[p.ID] = p
	i.order = append(i.order, p.ID)
	snap := Snapshot{Tick: i.tick, MapID: i.m.ID, Players: make([]PlayerView, 0, len(i.order)), Mobs: make([]MobView, 0, len(i.mobs)), Drops: i.dropViews()}
	for _, id := range i.order {
		snap.Players = append(snap.Players, i.view(i.players[id]))
	}
	for _, mb := range i.mobs {
		snap.Mobs = append(snap.Mobs, mb.view())
	}
	if !j.sink.Send(snap) {
		i.remove(p.ID)
		j.done <- ErrBusy
		return
	}
	j.done <- nil
}

func (i *Instance) remove(id model.CharacterID) bool {
	p, ok := i.players[id]
	if !ok {
		return false
	}
	i.checkpoint(p)
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x model.CharacterID) bool { return x == id })
	return true
}

// Input queue bounds (step 18): a burst after a stall is cut to the newest
// few, so the player catches up within ticks rather than lagging for good.
const (
	inputQueueMax  = 4
	inputQueueKeep = 2
)

// enqueueInput adds an input to a player's queue in sequence order,
// ignoring one already applied or already queued. The queue grows by at
// most one tick's drain before simulate cuts it.
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

// cutInputs drops a backlog longer than inputQueueMax down to the newest
// inputQueueKeep, once per tick after the drain, so a burst after a stall
// is caught up within a few ticks.
func cutInputs(q []inputMsg) []inputMsg {
	if len(q) > inputQueueMax {
		return slices.Delete(q, 0, len(q)-inputQueueKeep)
	}
	return q
}

// simulate applies exactly one queued input per player per tick, the
// oldest not yet applied (step 18), keeps the last held keys when none
// is queued, and runs the sub-steps. A dead player stays where they fell.
func (i *Instance) simulate() (changed []model.CharacterID) {
	for _, id := range i.order {
		p := i.players[id]
		p.pending = cutInputs(p.pending)
		if len(p.pending) > 0 {
			next := p.pending[0]
			p.held = next.in
			p.lastSeq = next.seq
			p.pending = slices.Delete(p.pending, 0, 1)
		}
		before := p.state
		if !p.dead {
			for range i.opts.SubSteps {
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

// broadcast sends each player its Delta and the tick's events. A player
// whose sink refuses an event is removed; those removals are returned.
func (i *Instance) broadcast(changed, removed []model.CharacterID) []model.CharacterID {
	for _, id := range i.order {
		if p := i.players[id]; (p.changed || p.moved) && !slices.Contains(changed, id) {
			changed = append(changed, id)
		}
	}
	views := make([]PlayerView, 0, len(changed))
	for _, id := range changed {
		if p, ok := i.players[id]; ok {
			views = append(views, i.view(p))
		}
	}
	var mobViews []MobView
	for _, mb := range i.mobs {
		if mb.changed {
			mobViews = append(mobViews, mb.view())
			mb.changed = false
		}
	}
	var dropped []model.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		d := Delta{Tick: i.tick, AckSeq: p.lastSeq, Players: views, Mobs: mobViews, Drops: slices.Clone(i.newDrops), Removed: removed, RemovedMobs: slices.Clone(i.removedMobs), RemovedDrops: slices.Clone(i.removedDrops)}
		ok := p.sink.Send(d)
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

func (i *Instance) view(p *player) PlayerView {
	return PlayerView{ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.derived.MaxHP, Dead: p.dead, State: p.state}
}

// checkpoint hands the player's state to the persistence worker without
// blocking; a full queue drops this checkpoint, the next sweep retries.
func (i *Instance) checkpoint(p *player) { i.checkpointOn(p, i.m.ID) }

func (i *Instance) ticksFor(d time.Duration) uint64 {
	n := uint64((d + i.opts.TickInterval - 1) / i.opts.TickInterval)
	if n == 0 {
		return 1
	}
	return n
}

// playerBox is the character's box in sim coordinates.
func (i *Instance) playerBox(p *player) combat.Rect {
	return combat.Rect{X: p.state.X - i.params.Width/2, Y: p.state.Y - i.params.Height, W: i.params.Width, H: i.params.Height}
}

func attackerOf(d character.Derived) combat.Attacker {
	return combat.Attacker{AttackPower: d.AttackPower, Accuracy: d.Accuracy, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration}
}

func defenderOf(d character.Derived) combat.Defender {
	return combat.Defender{Defense: d.Defense, Evasion: d.Evasion}
}

// detach drops a player without a checkpoint; the caller has saved one
// that names where the player went.
func (i *Instance) detach(id model.CharacterID) {
	delete(i.players, id)
	i.order = slices.DeleteFunc(i.order, func(x model.CharacterID) bool { return x == id })
}

// checkpointOn is checkpoint with the map the player will be on.
func (i *Instance) checkpointOn(p *player, mapID string) {
	if i.persist == nil {
		return
	}
	cp := character.Checkpoint{ID: p.ID, MapID: mapID, X: p.state.X, Y: p.state.Y, HP: p.HP, MP: p.MP, Exp: p.Exp}
	select {
	case i.persist <- Job{Checkpoint: &cp}:
	default:
	}
}
