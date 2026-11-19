// Package loot runs a map's pouches: rolled from kills, each its owner's alone, and taken item by item through the item module.
package loot

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/application/world/itemcalls"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
)

// Items is the item module, called from the item-call pool, never inside a tick.
type Items interface {
	// AddMany grants one owner's items in one call, answering each.
	AddMany(ctx context.Context, c model.GrantCall) (model.Granted, error)
	// Retryable reports whether a failed call is transient.
	Retryable(err error) bool
}

// Observer sees how many items each grant call carries.
type Observer interface {
	ObserveBatch(items int)
}

// Sink is where an owner who left the map hears of their pouches; Send must not block.
type Sink interface {
	Send(model.Event) bool
}

// Owner is who a pouch is for: the character, the account that scopes its grants' keys, the name their party sees, and where they hear of it once they left the map.
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
	Data      *gamedata.Data       // the loot rules, and the item templates equipment rolls its stats from
	// Rest is the ground a pouch at (x, z) from feet at y lies on, false where it'd be off the map or in a solid.
	Rest func(x, z, y float64) (float64, bool)
	// Collects reports whether an active quest of the owner's collects the item: a kill that leaves no pouch still rolls those.
	Collects func(owner kernel.CharacterID, item string) bool
	Logger   *slog.Logger
	// Settle takes a grant call's Result back to the map's tick; it runs on a worker, and must return once the map has stopped.
	Settle func(ctx context.Context, r Result)
}

// Tuning is the map's tick, and how many items one grant call may carry.
type Tuning struct {
	TickInterval time.Duration
	BatchMax     int
}

// entry is an item in a pouch, in memory only until its grant commits.
type entry struct {
	id        uint32    // its own within the pouch
	key       uuid.UUID // the idempotency key of its grant, the same on every try
	template  string
	quantity  int
	stats     *progression.ItemStats // equipment's roll, nil for anything else
	taking    bool                   // picked: in its owner's call, or in the take waiting for it
	unsettled bool                   // its call got no answer, so it may be granted: asked again under its key
}

// pouch is a kill's pouch or a boss's box on the ground, its owner's alone.
type pouch struct {
	id      uint64
	kind    model.PouchKind
	owner   kernel.CharacterID
	x, y, z float64
	items   []*entry
	goneAt  uint64
	gone    bool // its life ended: hidden from its owner, kept only for items still in a call
}

func (p *pouch) view(tick uint64, interval time.Duration) model.PouchView {
	items := make([]model.PouchItem, 0, len(p.items))
	for _, e := range p.items {
		items = append(items, model.PouchItem{ID: e.id, Template: e.template, Quantity: e.quantity, Stats: e.stats})
	}
	left := time.Duration(p.goneAt-min(tick, p.goneAt)) * interval
	return model.PouchView{ID: p.id, Kind: p.kind, X: p.x, Y: p.y, Z: p.z, Items: items, GoneIn: left}
}

func (p *pouch) entry(id uint32) *entry {
	if k := slices.IndexFunc(p.items, func(e *entry) bool { return e.id == id }); k >= 0 {
		return p.items[k]
	}
	return nil
}

// owner is an owner's pouches and calls: at most one call in flight, and at most one take waiting for it.
type owner struct {
	Owner
	pouches int // on the ground, gone ones still holding items in a call included
	busy    bool
	asking  []*ask // the takes the call in flight answers
	waiting *take  // the take sent once it settles
	retryAt uint64 // when items whose call got no answer go again
}

// Ground is one map's pouches; only the map's tick goroutine touches it, and its grant calls run on the item-call pool.
type Ground struct {
	roll   model.Roll
	deps   Deps
	tuning Tuning
	tracer trace.Tracer
	rules  gamedata.Loot
	// The lives and the retry in ticks.
	pouchLife, boxLife, retry, turnIdle uint64

	tick    uint64
	pouches []*pouch // oldest first
	nextID  uint64
	owners  map[kernel.CharacterID]*owner
	turns   map[string]*turn                      // by party
	changes map[kernel.CharacterID]*model.Pouches // this tick's, by owner
}

// NewGround is a map's empty ground.
func NewGround(d Deps, t Tuning) *Ground {
	ticks := func(d time.Duration) uint64 { return model.TicksIn(d, t.TickInterval) }
	l := d.Data.Loot()
	return &Ground{
		roll: d.Roll, deps: d, tuning: t, tracer: d.Traces.Tracer(tracerName), rules: l,
		pouchLife: ticks(l.PouchLife), boxLife: ticks(l.BoxLife), retry: ticks(retryEvery), turnIdle: ticks(turnIdle),
		owners: map[kernel.CharacterID]*owner{}, turns: map[string]*turn{}, changes: map[kernel.CharacterID]*model.Pouches{},
	}
}

// Begin starts tick n, forgetting the last tick's changes.
func (g *Ground) Begin(n uint64) {
	g.tick = n
	clear(g.changes)
}

// Kill is a mob's death as its pouch needs it: where it falls, and who may own it.
type Kill struct {
	Mob     gamedata.Mob
	X, Y, Z float64 // where it died
	Killer  Owner
	Party   model.Party // the killer's; the zero one alone
	// Eligible are the party's members alive on the map, the killer among them.
	Eligible []Owner
}

// Roll leaves one pouch where the mob died, for the next eligible party member in turn or the killer: holding the mob's whole roll when the pouch's chance passes, else only what an active quest of theirs collects, and nothing at all when nothing came of either.
func (g *Ground) Roll(k Kill) {
	to, take := g.next(k)
	chance := g.rules.Pouch.Normal
	if k.Mob.Elite {
		chance = g.rules.Pouch.Elite
	}
	whole := g.roll.Float64() < chance
	items := g.rolled(k.Mob, func(item string) bool { return whole || g.deps.Collects(to.ID, item) })
	if len(items) == 0 {
		return
	}
	take()
	g.place(model.PouchSmall, to, k.X, k.Y, k.Z, items, g.pouchLife)
}

// Box leaves the boss's whole roll in a box of the owner's own where it fell; it isn't gated, and party turns don't apply.
func (g *Ground) Box(boss gamedata.Mob, x, y, z float64, o Owner) {
	if items := g.rolled(boss, func(string) bool { return true }); len(items) > 0 {
		g.place(model.PouchBox, o, x, y, z, items, g.boxLife)
	}
}

// rolled is the mob's drop table rolled for each item wanted: its chance, quantity, and equipment's stats.
func (g *Ground) rolled(m gamedata.Mob, wanted func(item string) bool) []*entry {
	var out []*entry
	for _, d := range m.Drops {
		if !wanted(d.Item) || g.roll.Float64() >= d.Chance {
			continue
		}
		e := &entry{id: uint32(len(out) + 1), key: uuidv7.New(), template: d.Item, quantity: g.between(d.Min, d.Max)}
		if tmpl, ok := g.deps.Data.Item(d.Item); ok && tmpl.Tab == gamedata.Equip {
			rolled := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return g.between(r.Min, r.Max) })
			e.stats = &rolled
		}
		out = append(out, e)
	}
	return out
}

// place lays a pouch of items for o on the ground where something died, which its owner alone is told of.
func (g *Ground) place(kind model.PouchKind, o Owner, x, y, z float64, items []*entry, life uint64) {
	if ry, ok := g.deps.Rest(x, z, y); ok {
		y = ry
	}
	g.nextID++
	p := &pouch{id: g.nextID, kind: kind, owner: o.ID, x: x, y: y, z: z, items: items, goneAt: g.tick + life}
	g.pouches = append(g.pouches, p)
	st, ok := g.owners[o.ID]
	if !ok {
		st = &owner{}
		g.owners[o.ID] = st
	}
	st.Owner = o // their newest sink: their latest connection
	st.pouches++
	g.changed(p)
}

// between is a whole number from lo to hi, both included, uniformly; a range of one number draws nothing.
func (g *Ground) between(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return min(lo+int(g.roll.Float64()*float64(hi-lo+1)), hi)
}

// changed tells p's owner its view in this tick's frame, the latest of the tick standing.
func (g *Ground) changed(p *pouch) {
	c := g.of(p.owner)
	v := p.view(g.tick, g.tuning.TickInterval)
	if k := slices.IndexFunc(c.Changed, func(o model.PouchView) bool { return o.ID == p.id }); k >= 0 {
		c.Changed[k] = v
		return
	}
	c.Changed = append(c.Changed, v)
}

// vanish tells p's owner it's gone.
func (g *Ground) vanish(p *pouch) {
	c := g.of(p.owner)
	c.Changed = slices.DeleteFunc(c.Changed, func(o model.PouchView) bool { return o.ID == p.id })
	c.Gone = append(c.Gone, p.id)
}

func (g *Ground) of(id kernel.CharacterID) *model.Pouches {
	c, ok := g.changes[id]
	if !ok {
		c = &model.Pouches{}
		g.changes[id] = c
	}
	return c
}

// Views are the owner's pouches on the ground, as their Snapshot shows them.
func (g *Ground) Views(id kernel.CharacterID) []model.PouchView {
	var out []model.PouchView
	for _, p := range g.pouches {
		if p.owner == id && !p.gone {
			out = append(out, p.view(g.tick, g.tuning.TickInterval))
		}
	}
	return out
}

// Changes are this tick's changes to the owner's pouches, which their frame alone carries.
func (g *Ground) Changes(id kernel.CharacterID) model.Pouches {
	c, ok := g.changes[id]
	if !ok {
		return model.Pouches{}
	}
	return model.Pouches{Changed: slices.Clone(c.Changed), Gone: slices.Clone(c.Gone)}
}

// Step ends the lives that ran out, with what was left in them, and asks again for items whose call got no answer; it returns what to tell their owners.
func (g *Ground) Step() []Settled {
	var out []Settled
	for _, p := range g.pouches {
		if p.gone || p.goneAt > g.tick {
			continue
		}
		var left []model.LootItem
		p.items = slices.DeleteFunc(p.items, func(e *entry) bool {
			if e.taking {
				return false
			}
			left = append(left, model.LootItem{Template: e.template, Quantity: e.quantity})
			return true
		})
		p.gone = true
		g.vanish(p)
		if len(left) > 0 {
			out = append(out, Settled{Owner: g.owners[p.owner].Owner, Loot: []model.Loot{g.loot(p, model.LootLeft, left)}})
		}
	}
	g.sweep()
	g.retryUnsettled()
	g.forgetIdleTurns()
	return out
}

// sweep takes away the pouches gone or emptied that no call still holds, and forgets owners with none left.
func (g *Ground) sweep() {
	g.pouches = slices.DeleteFunc(g.pouches, func(p *pouch) bool {
		if len(p.items) > 0 {
			return false
		}
		if !p.gone {
			g.vanish(p)
		}
		o := g.owners[p.owner]
		o.pouches--
		if o.pouches == 0 && !o.busy && o.waiting == nil {
			delete(g.owners, p.owner)
		}
		return true
	})
}

func (g *Ground) loot(p *pouch, kind model.LootKind, items []model.LootItem) model.Loot {
	return model.Loot{Tick: g.tick, Kind: kind, MapID: g.deps.MapID, Pouch: p.id, Owner: p.owner, OwnerName: g.owners[p.owner].Name, Items: items}
}

func (g *Ground) find(id uint64) *pouch {
	if k := slices.IndexFunc(g.pouches, func(p *pouch) bool { return p.id == id }); k >= 0 {
		return g.pouches[k]
	}
	return nil
}
