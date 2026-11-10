// Package loot runs a map's drops: rolled from kills, each owned by one player from the moment it falls, and collected for them through the item service.
package loot

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/model"
)

const (
	dropSpread  = 24.0 // px either side of the mob
	dropSpacing = 20.0 // px between several drops of one mob
)

// Items is the item service, called from the item-call pool, never inside a tick.
type Items interface {
	// AddMany grants one owner's drops in one call, answering each drop.
	AddMany(ctx context.Context, c model.GrantCall) (model.Granted, error)
	// Retryable reports whether a failed call is transient.
	Retryable(err error) bool
}

// Observer sees how many drops each grant call carries.
type Observer interface {
	ObserveBatch(drops int)
}

// Sink is where an owner who left the map hears of their drops; Send must not block.
type Sink interface {
	Send(model.Event) bool
}

// Owner is who a drop goes to: the character, the account that scopes its grant's key, the name their party sees, and where they hear of it once they left the map.
type Owner struct {
	ID      kernel.CharacterID
	Account kernel.AccountID
	Name    string
	Sink    Sink
}

// Deps are what a map's ground calls; all are required.
type Deps struct {
	MapID     string
	Items     Items
	ItemCalls *itemcalls.Pool
	Observer  Observer
	Traces    trace.TracerProvider // starts each grant call's trace
	Roll      model.Roll           // the map's random source, used only on its tick goroutine
	Data      *gamedata.Data       // the loot timings, and the item templates equipment rolls its stats from
	Logger    *slog.Logger
	// Settle takes a grant call's Result back to the map's tick; it runs on a worker, and must return once the map has stopped.
	Settle func(ctx context.Context, r Result)
}

// Tuning is the map's tick, and how many drops one grant call may carry.
type Tuning struct {
	TickInterval time.Duration
	BatchMax     int
}

// drop is an item on the ground, in memory only until its grant commits.
type drop struct {
	id        uint64    // the wire ID
	key       uuid.UUID // the idempotency key of its grant, the same on every try
	template  string
	quantity  int
	stats     *progression.ItemStats // equipment's roll, nil for anything else
	x, y      float64
	owner     kernel.CharacterID
	collectAt uint64 // it falls until then
	expiresAt uint64
	state     state
	told      bool // its owner heard it waits for room
	last      bool // its life ended: one more try under its key, then it is lost
}

type state int

const (
	fresh     state = iota // never answered: falling until collectAt, then due
	sent                   // in its owner's call in flight
	noRoom                 // refused for room; tried again at its owner's room check
	unsettled              // its call failed, so it may be granted; tried again under its key
)

func (d *drop) view() model.DropView {
	return model.DropView{ID: d.id, Template: d.template, Quantity: d.quantity, X: d.x, Y: d.y, Owner: d.owner}
}

// owner is an owner's grant calls: at most one in flight, and when the drops waiting for room or an answer go again.
type owner struct {
	Owner
	drops   int // on the ground
	busy    bool
	retryAt uint64
}

// Ground is one map's drops; only the map's tick goroutine touches it, and its grant calls run on the item-call pool.
type Ground struct {
	roll   model.Roll
	deps   Deps
	tuning Tuning
	tracer trace.Tracer
	// The loot timings in ticks.
	collectDelay, life, roomCheck, turnIdle uint64

	tick      uint64
	drops     []*drop // oldest first
	nextID    uint64
	owners    map[kernel.CharacterID]*owner
	turns     map[string]*turn // by party
	added     []model.DropView // this tick's
	removed   []uint64
	collected []uint64
}

// NewGround is a map's empty ground.
func NewGround(d Deps, t Tuning) *Ground {
	ticks := func(d time.Duration) uint64 { return model.TicksIn(d, t.TickInterval) }
	l := d.Data.Loot()
	return &Ground{
		roll: d.Roll, deps: d, tuning: t, tracer: d.Traces.Tracer(tracerName),
		collectDelay: ticks(l.CollectDelay), life: ticks(l.DropLife), roomCheck: ticks(l.RoomCheckEvery), turnIdle: ticks(turnIdle),
		owners: map[kernel.CharacterID]*owner{}, turns: map[string]*turn{},
	}
}

// Begin starts tick n, forgetting the last tick's changes.
func (g *Ground) Begin(n uint64) {
	g.tick = n
	g.added = g.added[:0]
	g.removed = g.removed[:0]
	g.collected = g.collected[:0]
}

// Kill is a mob's death as its drops need it: where they fall, and who may own them.
type Kill struct {
	Mob    gamedata.Mob
	X, Y   float64 // where it died
	Killer Owner
	Party  model.Party // the killer's; the zero one alone
	// Eligible are the party's members alive on the map, the killer among them.
	Eligible []Owner
}

// Roll drops what the mob's table gives around where it died, each owned by the next eligible party member in turn, or by the killer alone.
func (g *Ground) Roll(k Kill) {
	n := 0
	for _, entry := range k.Mob.Drops {
		if g.roll.Float64() >= entry.Chance {
			continue
		}
		qty := g.between(entry.Min, entry.Max)
		var stats *progression.ItemStats
		if tmpl, ok := g.deps.Data.Item(entry.Item); ok && tmpl.Tab == gamedata.Equip {
			rolled := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return g.between(r.Min, r.Max) })
			stats = &rolled
		}
		to := g.next(k)
		g.own(to)
		g.nextID++
		d := &drop{
			id: g.nextID, key: uuidv7.New(), template: entry.Item, quantity: qty, stats: stats,
			x: k.X + (g.roll.Float64()*2-1)*dropSpread + float64(n)*dropSpacing, y: k.Y, owner: to.ID,
			collectAt: g.tick + g.collectDelay, expiresAt: g.tick + g.life,
		}
		g.drops = append(g.drops, d)
		g.added = append(g.added, d.view())
		n++
	}
}

// own counts a drop for o, whose newest sink is their latest connection.
func (g *Ground) own(o Owner) {
	st, ok := g.owners[o.ID]
	if !ok {
		st = &owner{}
		g.owners[o.ID] = st
	}
	st.Owner = o
	st.drops++
}

// between is a whole number from lo to hi, both included, uniformly; a range of one number draws nothing.
func (g *Ground) between(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return min(lo+int(g.roll.Float64()*float64(hi-lo+1)), hi)
}

// Views are every drop, as a Snapshot shows them.
func (g *Ground) Views() []model.DropView {
	out := make([]model.DropView, 0, len(g.drops))
	for _, d := range g.drops {
		out = append(out, d.view())
	}
	return out
}

// Changes are the drops added this tick, those gone without an owner taking them, and those collected.
func (g *Ground) Changes() (added []model.DropView, removed, collected []uint64) {
	return slices.Clone(g.added), slices.Clone(g.removed), slices.Clone(g.collected)
}
