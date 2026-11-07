// Package loot runs a map's drops: rolled from kills, its killer's party's for a while, granted by the item service.
package loot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/model"
)

const (
	lootWindow  = 10 * time.Second // only the killer and their party may take it
	dropLife    = 60 * time.Second
	pickupRange = 40.0 // px from the character's centre
	dropSpread  = 24.0 // px either side of the mob
	dropSpacing = 20.0 // px between several drops of one mob
	callTimeout = 5 * time.Second
)

// Pickups is the item service, called from economy workers, never inside a tick.
type Pickups interface {
	Add(ctx context.Context, p model.Pickup) ([]model.Slot, error)
	// Retryable reports whether a failed Add is transient (a full inventory is not).
	Retryable(err error) bool
	// Granted reports whether a failed Add means an earlier try, whose reply was lost, granted the drop.
	Granted(err error) bool
	// Inventory is the character's whole inventory, for a grant whose own reply was lost.
	Inventory(ctx context.Context, id kernel.CharacterID) ([]model.Slot, error)
}

// Parties tells whether a picker shares the killer's party, from inside a tick.
type Parties interface {
	SameParty(a, b kernel.CharacterID) bool
}

// Deps are what a map's ground calls; all are required.
type Deps struct {
	Items   Pickups
	Workers *economy.Workers
	Parties Parties
	Traces  trace.TracerProvider // starts each pickup's trace
	Roll    model.Roll           // the map's random source, used only on its tick goroutine
}

// Tuning is the map's tick.
type Tuning struct {
	TickInterval time.Duration
}

// drop is an item on the ground, in memory only until a pickup commits.
type drop struct {
	id         uint64    // the wire ID
	key        uuid.UUID // the idempotency key of its pickup
	template   string
	quantity   int
	x, y       float64
	killer     kernel.CharacterID
	windowEnd  uint64 // tick until which only the killer may take it
	expiresAt  uint64
	reservedBy kernel.CharacterID // zero when free
	// unsettled: its pickup's outcome is unknown, so only reservedBy retries it, as the item service keys per account.
	unsettled bool
	last      *pickup // the latest pickup, which an unsettled drop tries once more as it expires
}

// pickup is one grant of a drop: what to ask the item service, the command it links to, and who hears the outcome.
type pickup struct {
	drop  uint64
	in    model.Pickup
	cause trace.SpanContext
	done  func(ctx context.Context, r Result)
}

func (d *drop) view() model.DropView {
	return model.DropView{ID: d.id, Template: d.template, Quantity: d.quantity, X: d.x, Y: d.y}
}

// Ground is one map's drops; only the map's tick goroutine touches it, and its pickups run on the economy's workers.
type Ground struct {
	roll     model.Roll
	interval time.Duration // the map's tick
	deps     Deps
	tracer   trace.Tracer

	tick    uint64
	drops   []*drop
	nextID  uint64
	added   []model.DropView // this tick's
	removed []uint64
}

// NewGround is a map's empty ground.
func NewGround(d Deps, t Tuning) *Ground {
	return &Ground{roll: d.Roll, interval: t.TickInterval, deps: d, tracer: d.Traces.Tracer(tracerName)}
}

// Begin starts tick n, forgetting the last tick's changes.
func (g *Ground) Begin(n uint64) {
	g.tick = n
	g.added = g.added[:0]
	g.removed = g.removed[:0]
}

// Roll drops what mob's table gives around where it died at (x, y), killer's to take first.
func (g *Ground) Roll(mob gamedata.Mob, x, y float64, killer kernel.CharacterID) {
	n := 0
	for _, entry := range mob.Drops {
		if g.roll.Float64() >= entry.Chance {
			continue
		}
		qty := entry.Min
		if entry.Max > entry.Min {
			qty += int(g.roll.Float64() * float64(entry.Max-entry.Min+1))
			qty = min(qty, entry.Max)
		}
		g.nextID++
		d := &drop{
			id: g.nextID, key: uuidv7.New(), template: entry.Item, quantity: qty,
			x: x + (g.roll.Float64()*2-1)*dropSpread + float64(n)*dropSpacing, y: y, killer: killer,
			windowEnd: g.tick + model.TicksIn(lootWindow, g.interval), expiresAt: g.tick + model.TicksIn(dropLife, g.interval),
		}
		g.drops = append(g.drops, d)
		g.added = append(g.added, d.view())
		n++
	}
}

// Expire takes away the drops past their life that no pickup holds; an unsettled one goes with a last grant under its key.
func (g *Ground) Expire() {
	g.drops = slices.DeleteFunc(g.drops, func(d *drop) bool {
		if d.expiresAt > g.tick || !d.reservedBy.IsZero() && !d.unsettled {
			return false
		}
		if d.unsettled && !g.submit(*d.last, true) {
			return false // no worker free for its last try; the next tick asks again
		}
		g.removed = append(g.removed, d.id)
		return true
	})
}

// Picker is a player reaching for a drop, from their centre.
type Picker struct {
	ID      kernel.CharacterID
	Account kernel.AccountID
	X, Y    float64
	Cause   trace.SpanContext // the command that asked, which the pickup's trace links to
}

// Result is a pickup's outcome, which the map settles on its tick.
type Result struct {
	Who   kernel.CharacterID
	drop  uint64
	slots []model.Slot
	err   error
	last  bool // the try an unsettled drop made as it expired
}

// PickUp reserves the nearest drop p may take for a worker to grant, which hands done the Result; else ErrPickupBusy.
func (g *Ground) PickUp(p Picker, done func(ctx context.Context, r Result)) error {
	var best *drop
	bestDist := math.Inf(1)
	for _, d := range g.drops {
		if !d.reservedBy.IsZero() && (!d.unsettled || d.reservedBy != p.ID) {
			continue
		}
		if d.windowEnd > g.tick && d.killer != p.ID && !g.deps.Parties.SameParty(d.killer, p.ID) {
			continue // someone else's loot for now (the killer's party may)
		}
		if dist := math.Hypot(d.x-p.X, d.y-p.Y); dist <= pickupRange && dist < bestDist {
			best, bestDist = d, dist
		}
	}
	if best == nil {
		return nil
	}
	in := model.Pickup{Account: p.Account, Character: p.ID, DropID: best.key, Template: best.template, Quantity: best.quantity}
	pu := pickup{drop: best.id, in: in, cause: p.Cause, done: done}
	if !g.submit(pu, false) {
		return model.ErrPickupBusy
	}
	best.reservedBy, best.unsettled, best.last = p.ID, false, &pu
	return nil
}

// submit hands a worker the grant of pu, false when none can take it.
func (g *Ground) submit(pu pickup, last bool) bool {
	return g.deps.Workers.Submit(func(ctx context.Context) {
		r := g.grant(ctx, pu)
		r.last = last
		pu.done(ctx, r)
	})
}

const tracerName = "finga/services/world/internal/application/loot"

// pickupSpan names a pickup's trace: the call a tick hands to a worker.
const pickupSpan = "world.pickup"

// EntryPoint reports whether a root span of this name starts a trace that is kept: a pickup's.
func EntryPoint(span string) bool { return span == pickupSpan }

// grant asks the item service for the drop, on a worker, retrying a transient failure once under the same key.
func (g *Ground) grant(ctx context.Context, pu pickup) Result {
	in := pu.in
	opts := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithAttributes(
		attribute.String("character_id", in.Character.String()), attribute.String("drop_id", in.DropID.String()))}
	if pu.cause.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: pu.cause}))
	}
	ctx, span := g.tracer.Start(ctx, pickupSpan, opts...)
	defer span.End()
	slots, err := g.add(ctx, in)
	if err != nil && g.deps.Items.Retryable(err) {
		slots, err = g.add(ctx, in) // the key makes a retry harmless
		if err != nil && g.deps.Items.Retryable(err) {
			err = fmt.Errorf("%w: %w", model.ErrPickupsUnavailable, err)
		}
	}
	if err != nil && g.deps.Items.Granted(err) {
		slots, err = g.held(ctx, in), nil // granted, though the slots it changed went with the lost reply
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return Result{Who: in.Character, drop: pu.drop, slots: slots, err: err}
}

func (g *Ground) add(ctx context.Context, in model.Pickup) ([]model.Slot, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return g.deps.Items.Add(ctx, in)
}

// held is the picker's slots holding the drop's item, where its grant landed, so other slots a newer change moved stay as told; nothing when the read fails.
func (g *Ground) held(ctx context.Context, in model.Pickup) []model.Slot {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	slots, err := g.deps.Items.Inventory(ctx, in.Character)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(slots, func(s model.Slot) bool { return s.Item == nil || s.Item.Template != in.Template })
}

// Settle applies a pickup's outcome, removing, freeing or holding its drop; it returns what to tell the picker.
func (g *Ground) Settle(r Result) (model.Event, bool) {
	k := slices.IndexFunc(g.drops, func(d *drop) bool { return d.id == r.drop })
	if r.err != nil && r.last {
		return nil, false // the drop is gone, and the picker heard of the failure already
	}
	if r.err != nil {
		if k >= 0 && g.drops[k].reservedBy == r.Who {
			if errors.Is(r.err, model.ErrPickupsUnavailable) {
				g.drops[k].unsettled = true // the add may have committed before its reply was lost
			} else {
				g.drops[k].reservedBy, g.drops[k].last = kernel.CharacterID{}, nil
			}
		}
		return model.PickupFailed{Tick: g.tick, Err: r.err}, true
	}
	if k >= 0 {
		g.drops = slices.Delete(g.drops, k, k+1)
		g.removed = append(g.removed, r.drop)
	}
	if len(r.slots) == 0 {
		return nil, false
	}
	return model.InventoryChanged{Tick: g.tick, Slots: r.slots}, true
}

// Views are every drop, as a Snapshot shows them.
func (g *Ground) Views() []model.DropView {
	out := make([]model.DropView, 0, len(g.drops))
	for _, d := range g.drops {
		out = append(out, d.view())
	}
	return out
}

// Changes are the drops added and removed this tick.
func (g *Ground) Changes() (added []model.DropView, removed []uint64) {
	return slices.Clone(g.added), slices.Clone(g.removed)
}
