// Package instance runs one map as an actor: its inbox, its tick, its players, and the mobs and drops among them.
package instance

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"time"

	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/tick"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
)

const (
	drainLimit = 1024 // inbox messages handled per tick; the rest wait
	inboxSize  = 4096
)

// Tuning is how often a map ticks and checkpoints its players, and how many drops one grant call may carry.
type Tuning struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	BatchMax           int
}

// Services are what every map of a World calls; all are required.
type Services struct {
	NewRoll   func() model.Roll // each map's random source, used only on its tick goroutine
	Observer  TickObserver
	Parties   Parties
	Items     loot.Items
	Batches   loot.Observer
	Equipment Equipment
	HandIns   HandIns
	Potions   Potions
	ItemCalls *itemcalls.Pool
	Traces    trace.TracerProvider // starts each grant call's trace
}

// Deps are one map's: the map, where its checkpoints go and who takes its players to other maps, with the shared services; all are required.
type Deps struct {
	Services
	Map    *gamedata.Map
	Data   *gamedata.Data
	Saves  Checkpoints
	Mover  Mover
	Logger *slog.Logger // the map's log: for the tick, such as a rescued fall, and for work off it with no one else to tell, such as a grant call
}

// Instance is one map's actor; only its tick goroutine touches its state.
type Instance struct {
	m        *gamedata.Map
	data     *gamedata.Data
	params   sim.Params
	subSteps int // sim steps per tick
	inputs   inputTicks
	epoch    uint64 // the party stream's subscription the players were last told to
	tuning   Tuning
	deps     Services
	log      *slog.Logger
	mover    Mover
	inbox    chan message
	stopped  chan struct{} // closed once Run has returned: nothing drains the inbox
	saves    Checkpoints
	arena    *mobs.Arena
	ground   *loot.Ground
	roll     model.Roll // the map's random source, which equipment rewards roll from as drops do

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
	}
	leaveMsg struct {
		id   kernel.CharacterID
		conn string // empty: any
		done chan struct{}
	}
	inputMsg struct {
		id  kernel.CharacterID
		seq uint32
		in  sim.Input
	}
	attackMsg struct{ id kernel.CharacterID }
	settleMsg struct{ result loot.Result }
	portalMsg struct{ id kernel.CharacterID }
	// equipMsg asks to equip or unequip, whichever is set; done hears the outcome once it applies.
	equipMsg struct {
		id      kernel.CharacterID
		conn    string
		equip   *model.EquipCall
		unequip *model.UnequipCall
		done    chan error
	}
	equippedMsg struct {
		id     kernel.CharacterID
		caller *player // the join that made the call, whose count of calls in flight it ends
		result model.Equipped
		err    error
		done   chan error
	}
	// useMsg asks to drink a potion; done hears the outcome once it applies.
	useMsg struct {
		id   kernel.CharacterID
		conn string
		call model.UseCall
		done chan error
	}
	usedMsg struct {
		id       kernel.CharacterID
		caller   *player // the join that made the call, whose count of calls in flight it ends
		call     model.UseCall
		result   model.Inventory
		replayed bool
		err      error
		done     chan error
	}
	chatMsg struct {
		from kernel.CharacterID
		name string
		text string
	}
)

func (*joinMsg) message()    {}
func (leaveMsg) message()    {}
func (inputMsg) message()    {}
func (attackMsg) message()   {}
func (settleMsg) message()   {}
func (portalMsg) message()   {}
func (equipMsg) message()    {}
func (equippedMsg) message() {}
func (chatMsg) message()     {}
func (useMsg) message()      {}
func (usedMsg) message()     {}

// Errors a join can end with besides the model's.
var (
	// ErrSinkClosed is a join whose Snapshot its sink refused: the connection's stream is gone.
	ErrSinkClosed = errors.New("the sink is closed")
)

// New builds the map's actor, which Run starts.
func New(d Deps, t Tuning) *Instance {
	params := d.Data.Movement()
	roll := d.NewRoll()
	i := &Instance{
		m: d.Map, data: d.Data, params: params, tuning: t, deps: d.Services, log: d.Logger, mover: d.Mover, saves: d.Saves,
		subSteps: max(1, int(math.Round(t.TickInterval.Seconds()/params.Dt))),
		inputs:   inputTicksAt(t.TickInterval),
		inbox:    make(chan message, inboxSize),
		stopped:  make(chan struct{}),
		arena:    mobs.NewArena(mobs.Deps{Map: d.Map, Data: d.Data, Roll: roll}, mobs.Tuning{TickInterval: t.TickInterval}),
		players:  map[kernel.CharacterID]*player{},
		ticksPer: max(uint64(t.CheckpointInterval/t.TickInterval), 1),
		roll:     roll,
	}
	i.ground = loot.NewGround(loot.Deps{
		MapID: d.Map.ID, Items: d.Items, ItemCalls: d.ItemCalls, Observer: d.Batches, Traces: d.Traces, Roll: roll, Data: d.Data, Logger: d.Logger, Settle: i.settled,
	}, loot.Tuning{TickInterval: t.TickInterval, BatchMax: t.BatchMax})
	return i
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

// Join adds a player, logging in or handed over by another map, and waits until its Snapshot is sent; ErrBusy when the inbox is full, ErrStopped once stopped.
func (i *Instance) Join(ctx context.Context, p model.Player, sink Sink) error {
	done := make(chan error, 1)
	if err := i.post(&joinMsg{player: p, sink: sink, done: done}); err != nil {
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

// Leave removes the player who joined through conn (empty: any); done, if set, closes once it is handled.
func (i *Instance) Leave(id kernel.CharacterID, conn string, done chan struct{}) error {
	return i.post(leaveMsg{id: id, conn: conn, done: done})
}

// Input queues held keys without blocking; a dropped input is superseded by the next.
func (i *Instance) Input(id kernel.CharacterID, seq uint32, in sim.Input) error {
	return i.post(inputMsg{id: id, seq: seq, in: in})
}

// Attack queues a basic attack; the tick enforces the cooldown.
func (i *Instance) Attack(id kernel.CharacterID) error {
	return i.post(attackMsg{id: id})
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
	if n%i.ticksFor(time.Second) == 0 {
		i.regen()
	}
	for _, s := range i.ground.Collect() {
		i.settle(s)
	}
	i.tellHP()
	i.tellQuests()
	for _, id := range i.order {
		if p := i.players[id]; p.slot == n%i.ticksPer {
			i.checkpoint(p)
		}
	}
	i.retellParties()
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
		case settleMsg:
			i.settle(i.ground.Settle(m.result))
		case portalMsg:
			if p, ok := i.players[m.id]; ok && i.enterPortal(p) {
				removed = append(removed, m.id)
			}
		case equipMsg:
			i.startEquip(m)
		case equippedMsg:
			i.settleEquip(m)
		case pointsMsg:
			i.settlePoints(m)
		case skillPointsMsg:
			i.settleSkillPoints(m)
		case talkMsg:
			i.talk(m)
		case questMsg:
			i.quest(m)
		case handedInMsg:
			i.settleHandIn(m)
		case useMsg:
			i.startUse(m)
		case usedMsg:
			i.settleUse(m)
		case chatMsg:
			if _, ok := i.players[m.from]; ok {
				i.events = append(i.events, model.Chat{Tick: i.tick, Sender: m.from, SenderName: m.name, Text: m.text})
			}
		case leaveMsg:
			if p, ok := i.players[m.id]; ok && (m.conn == "" || p.Connection == m.conn) {
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

// settled posts a grant call's answer to the tick, unless the map has stopped and nothing drains the inbox.
func (i *Instance) settled(ctx context.Context, r loot.Result) {
	select {
	case i.inbox <- settleMsg{result: r}:
	case <-i.stopped:
	case <-ctx.Done():
	}
}

// settle tells a drop's owner what came of it, and the party members on the map what it collected.
func (i *Instance) settle(s loot.Settled) {
	for _, e := range s.Inventory {
		i.tell(s.Owner, e)
	}
	for _, l := range s.Loot {
		i.tell(s.Owner, l)
		if l.Kind != model.LootCollected {
			continue
		}
		for _, id := range i.deps.Parties.Party(s.Owner.ID).Members {
			if p, ok := i.players[id]; ok && id != s.Owner.ID {
				p.private = append(p.private, l)
			}
		}
	}
}

// tell sends a drop's owner an event of their own, with the tick's broadcast, or now through their sink if they left the map.
func (i *Instance) tell(o loot.Owner, e model.Event) {
	if p, ok := i.players[o.ID]; ok {
		p.private = append(p.private, e)
		return
	}
	o.Sink.Send(e) // they left through a portal or a death since, or logged out
}
