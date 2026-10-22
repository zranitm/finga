package world

import (
	"context"
	"slices"
	"time"

	"finga/internal/application/character"
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
)

// Options tune the instances. Zero values take the defaults.
type Options struct {
	TickInterval       time.Duration
	SubSteps           int // movement sub-steps per tick (page 5: 3 of 1/60 s)
	DrainLimit         int // inbox messages handled per tick; the rest wait
	InboxSize          int
	CheckpointInterval time.Duration // TD-02
	Observer           TickObserver  // nil: no metrics
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
	return o
}

// Instance is one map's actor. Everything inside it runs on its tick
// goroutine; other goroutines only send to its inbox.
type Instance struct {
	m        *gamedata.Map
	params   sim.Params
	opts     Options
	inbox    chan message
	persist  chan<- character.Checkpoint
	tick     uint64
	players  map[model.CharacterID]*player
	order    []model.CharacterID // join order, so iteration is deterministic
	ticksPer uint64              // ticks between checkpoint sweeps
	dropped  []model.CharacterID // removed during the last broadcast, reported next tick
}

// message is one inbox item.
type message struct {
	join  *joinMsg
	leave model.CharacterID
	input *inputMsg
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
// state, and the input queue.
type player struct {
	Player
	sink    Sink
	state   sim.State
	held    sim.Input
	pending []inputMsg // inputs received this tick, applied in sequence order
	lastSeq uint32
	moved   bool
	joined  bool
}

// NewInstance builds the actor for m. Run starts it.
func NewInstance(m *gamedata.Map, params sim.Params, persist chan<- character.Checkpoint, opts Options) *Instance {
	opts = opts.withDefaults()
	ticksPer := uint64(opts.CheckpointInterval / opts.TickInterval)
	if ticksPer == 0 {
		ticksPer = 1
	}
	return &Instance{
		m: m, params: params, opts: opts,
		inbox:    make(chan message, opts.InboxSize),
		persist:  persist,
		players:  map[model.CharacterID]*player{},
		ticksPer: ticksPer,
	}
}

// MapID is the map this instance runs.
func (i *Instance) MapID() string { return i.m.ID }

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

func (i *Instance) post(m message) bool {
	select {
	case i.inbox <- m:
		return true
	default:
		return false
	}
}

// Step is one tick (page 3): drain the inbox, apply inputs and simulate,
// then send each player its delta. Nothing here blocks or touches the
// database.
func (i *Instance) Step(n uint64, _ time.Duration) {
	i.tick = n
	removed := slices.Concat(i.dropped, i.drain())
	changed := i.simulate()
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
			if p, ok := i.players[m.input.id]; ok && m.input.seq > p.lastSeq {
				p.pending = append(p.pending, *m.input)
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
	p := &player{
		Player: j.player,
		sink:   j.sink,
		state:  sim.State{X: j.player.X, Y: j.player.Y, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform},
		joined: true,
	}
	i.players[p.ID] = p
	i.order = append(i.order, p.ID)
	snap := Snapshot{Tick: i.tick, MapID: i.m.ID, Players: make([]PlayerView, 0, len(i.order))}
	for _, id := range i.order {
		snap.Players = append(snap.Players, i.view(i.players[id]))
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

// simulate applies each player's pending inputs in sequence order (the
// last one is the held keys for this tick) and runs the sub-steps.
func (i *Instance) simulate() (changed []model.CharacterID) {
	for _, id := range i.order {
		p := i.players[id]
		if len(p.pending) > 0 {
			slices.SortStableFunc(p.pending, func(a, b inputMsg) int { return int(a.seq) - int(b.seq) })
			for _, in := range p.pending {
				if in.seq > p.lastSeq {
					p.held = in.in
					p.lastSeq = in.seq
				}
			}
			p.pending = p.pending[:0]
		}
		before := p.state
		for range i.opts.SubSteps {
			p.state = sim.Step(p.state, p.held, i.m.Geometry, i.params)
		}
		p.moved = p.state != before
		if p.moved || p.joined {
			changed = append(changed, id)
		}
	}
	return changed
}

// broadcast sends each player its Delta. A player whose sink refuses the
// event is removed; those removals are returned.
func (i *Instance) broadcast(changed, removed []model.CharacterID) []model.CharacterID {
	views := make([]PlayerView, 0, len(changed))
	for _, id := range changed {
		views = append(views, i.view(i.players[id]))
	}
	var dropped []model.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		d := Delta{Tick: i.tick, AckSeq: p.lastSeq, Players: views, Removed: removed}
		if !p.sink.Send(d) {
			i.remove(id)
			dropped = append(dropped, id)
		}
		p.joined = false
	}
	return dropped
}

func (i *Instance) view(p *player) PlayerView {
	return PlayerView{ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, HP: p.HP, MaxHP: p.MaxHP, State: p.state}
}

// checkpoint hands the player's state to the persistence worker without
// blocking; a full queue drops this checkpoint, the next sweep retries.
func (i *Instance) checkpoint(p *player) {
	if i.persist == nil {
		return
	}
	cp := character.Checkpoint{ID: p.ID, MapID: i.m.ID, X: p.state.X, Y: p.state.Y, HP: p.HP, MP: p.MP, Exp: p.Exp}
	select {
	case i.persist <- cp:
	default:
	}
}
