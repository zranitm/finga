// Package instance runs one map as an actor: its inbox, its tick, its players, and the mobs and drops among them.
package instance

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/tick"
	"finga/services/world/internal/application/combat"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/model"
)

const (
	drainLimit = 1024 // inbox messages handled per tick; the rest wait
	inboxSize  = 4096
)

// Tuning is how often a map ticks and checkpoints its players.
type Tuning struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
}

// Deps are the services every map calls; all are required.
type Deps struct {
	NewRoll  func() model.Roll // each map's random source, used only on its tick goroutine
	Observer TickObserver
	Parties  Parties
	Items    loot.Pickups
	Workers  *economy.Workers
	Traces   trace.TracerProvider // starts each pickup's trace
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
	inbox    chan message
	stopped  chan struct{} // closed once Run has returned: nothing drains the inbox
	saves    Checkpoints
	arena    *combat.Arena
	ground   *loot.Ground

	tick     uint64
	players  map[kernel.CharacterID]*player
	order    []kernel.CharacterID // join order, so iteration is deterministic
	ticksPer uint64               // ticks between a player's checkpoints
	nextSlot uint64               // the sweep tick, mod ticksPer, the next player to join gets

	dropped []kernel.CharacterID // removed during the last broadcast, reported next tick
	events  []model.Event        // map-wide events of the current tick
}

// message is what the tick drains from the inbox.
//
//sumtype:decl
type message interface{ message() }

type (
	joinMsg struct {
		player model.Player
		sink   Sink
		done   chan error
		held   func() bool // when set, whether the join's presence still holds the character
	}
	leaveMsg struct {
		id   kernel.CharacterID
		conn string // empty: any
		done chan struct{}
		took *Departure
	}
	inputMsg struct {
		id  kernel.CharacterID
		seq uint32
		in  sim.Input
	}
	attackMsg struct{ id kernel.CharacterID }
	pickMsg   struct{ id kernel.CharacterID }
	pickedMsg struct {
		result loot.Result
		sink   Sink // the picker's, for a result that arrives after they left the map
	}
	portalMsg struct{ id kernel.CharacterID }
	chatMsg   struct {
		from kernel.CharacterID
		name string
		text string
	}
)

func (*joinMsg) message()  {}
func (leaveMsg) message()  {}
func (inputMsg) message()  {}
func (attackMsg) message() {}
func (pickMsg) message()   {}
func (pickedMsg) message() {}
func (portalMsg) message() {}
func (chatMsg) message()   {}

// Errors a join can end with besides the model's.
var (
	// ErrSinkClosed is a join whose Snapshot its sink refused: the connection's stream is gone.
	ErrSinkClosed = errors.New("the sink is closed")
	// ErrMoved is a transfer whose presence left, or was replaced, before it landed.
	ErrMoved = errors.New("the presence moved on")
)

// New builds the actor for m; checkpoints go to saves, mover takes players to other maps, and Run starts it.
func New(m *gamedata.Map, data *gamedata.Data, saves Checkpoints, mover Mover, t Tuning, d Deps) *Instance {
	params := data.Movement()
	roll := d.NewRoll()
	return &Instance{
		m: m, data: data, params: params, tuning: t, deps: d, mover: mover, saves: saves,
		subSteps: max(1, int(math.Round(t.TickInterval.Seconds()/params.Dt))),
		inbox:    make(chan message, inboxSize),
		stopped:  make(chan struct{}),
		arena:    combat.NewArena(m, data, roll, t.TickInterval),
		ground:   loot.NewGround(roll, t.TickInterval, loot.Deps{Items: d.Items, Workers: d.Workers, Parties: d.Parties, Traces: d.Traces}),
		players:  map[kernel.CharacterID]*player{},
		ticksPer: max(uint64(t.CheckpointInterval/t.TickInterval), 1),
	}
}

// MapID is the map the instance runs.
func (i *Instance) MapID() string { return i.m.ID }

// Run ticks until ctx is done, then checkpoints the players still on the map.
func (i *Instance) Run(ctx context.Context) error {
	defer close(i.stopped)
	mapID := i.m.ID
	err := tick.Run(ctx, i.tuning.TickInterval, i, tick.WithObserver(func(_ uint64, took, interval time.Duration) {
		i.deps.Observer.ObserveTick(mapID, took, interval)
	}))
	for _, id := range i.order {
		i.checkpoint(i.players[id])
	}
	return err
}

// Join adds a player and waits until its Snapshot is sent; ErrBusy when the inbox is full, ErrStopped once stopped.
func (i *Instance) Join(ctx context.Context, p model.Player, sink Sink) error {
	return i.join(ctx, &joinMsg{player: p, sink: sink})
}

// Arrive joins a player another map handed over, unless held says its presence moved on first: ErrMoved.
func (i *Instance) Arrive(ctx context.Context, p model.Player, sink Sink, held func() bool) error {
	return i.join(ctx, &joinMsg{player: p, sink: sink, held: held})
}

func (i *Instance) join(ctx context.Context, j *joinMsg) error {
	done := make(chan error, 1)
	j.done = done
	if err := i.post(j); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-i.stopped:
		select {
		case err := <-done:
			return err
		default:
			return model.ErrStopped
		}
	}
}

// Departure is what a leave took off the map, filled in before its done closes.
type Departure struct {
	Sink Sink
	OK   bool // the player was on the map
}

// Leave removes the player who joined through conn (empty: any); done, if set, closes once it is handled and took filled.
func (i *Instance) Leave(id kernel.CharacterID, conn string, done chan struct{}, took *Departure) error {
	return i.post(leaveMsg{id: id, conn: conn, done: done, took: took})
}

// Input queues held keys without blocking; a dropped input is superseded by the next.
func (i *Instance) Input(id kernel.CharacterID, seq uint32, in sim.Input) error {
	return i.post(inputMsg{id: id, seq: seq, in: in})
}

// Attack queues a basic attack; the tick enforces the cooldown.
func (i *Instance) Attack(id kernel.CharacterID) error {
	return i.post(attackMsg{id: id})
}

// PickUp asks for the nearest eligible drop within reach.
func (i *Instance) PickUp(id kernel.CharacterID) error {
	return i.post(pickMsg{id: id})
}

// EnterPortal takes the portal the character stands in, if any.
func (i *Instance) EnterPortal(id kernel.CharacterID) error {
	return i.post(portalMsg{id: id})
}

// Chat says an already cleaned line to everyone on the map, the sender included.
func (i *Instance) Chat(from kernel.CharacterID, name, text string) error {
	return i.post(chatMsg{from: from, name: name, text: text})
}

// post queues m without blocking: ErrStopped once the map has stopped, ErrBusy when its inbox is full.
func (i *Instance) post(m message) error {
	select {
	case <-i.stopped:
		return model.ErrStopped
	default:
	}
	select {
	case i.inbox <- m:
		return nil
	default:
		return model.ErrBusy
	}
}

// Wait waits for done, which the map closes once it has handled a message: ErrStopped if it stops first.
func (i *Instance) Wait(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-i.stopped:
		select {
		case <-done:
			return nil
		default:
			return model.ErrStopped
		}
	}
}

// Step runs one tick. Nothing here may block or touch the database.
func (i *Instance) Step(n uint64, _ time.Duration) {
	i.tick = n
	i.events = i.events[:0]
	i.ground.Begin(n)
	removed := slices.Concat(i.dropped, i.drain())
	changed := i.simulate()
	i.arena.Step(n)
	i.resolveAttacks()
	i.contactDamage()
	removed = append(removed, i.deathsAndRespawns()...)
	i.ground.Expire()
	for _, id := range i.order {
		if p := i.players[id]; p.slot == n%i.ticksPer {
			i.checkpoint(p)
		}
	}
	i.dropped = i.broadcast(changed, i.gone(removed))
}

// gone keeps the removed players not back: a rejoin in one tick is an update, and the client applies removals last.
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
		case pickMsg:
			if p, ok := i.players[m.id]; ok {
				i.pickUp(p)
			}
		case pickedMsg:
			if e, ok := i.ground.Settle(m.result); ok {
				i.tell(m.result.Who, m.sink, e)
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
			if p, ok := i.players[m.id]; ok && (m.conn == "" || p.Connection == m.conn) {
				if m.took != nil {
					*m.took = Departure{Sink: p.sink, OK: true}
				}
				i.remove(m.id)
				removed = append(removed, m.id)
			}
			if m.done != nil {
				close(m.done)
			}
		}
	}
	return removed
}

// pickUp hands the nearest drop the player may take to a worker, whose result comes back through the inbox.
func (i *Instance) pickUp(p *player) {
	if p.dead {
		return
	}
	sink := p.sink
	picker := loot.Picker{ID: p.ID, Account: p.Account, X: p.state.X, Y: p.state.Y - i.params.Height/2}
	err := i.ground.PickUp(picker, func(ctx context.Context, r loot.Result) {
		select {
		case i.inbox <- pickedMsg{result: r, sink: sink}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if err != nil {
		p.private = append(p.private, model.PickupFailed{Tick: i.tick, Err: err})
	}
}

// tell sends a player an event of their own, with the tick's broadcast, or now through sink if they left the map.
func (i *Instance) tell(id kernel.CharacterID, sink Sink, e model.Event) {
	if p, ok := i.players[id]; ok {
		p.private = append(p.private, e)
		return
	}
	sink.Send(e) // they left through a portal or a death since; the connection is the same
}
