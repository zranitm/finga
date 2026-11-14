package loot

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/kernel"
	"finga/modules/world/internal/model"
)

const callTimeout = 5 * time.Second

const tracerName = "finga/modules/world/internal/application/loot"

// collectSpan names a grant call's trace: the job a tick hands to a worker.
const collectSpan = "world.collect"

// EntryPoint reports whether a root span of this name starts a trace that is kept: a grant call's.
func EntryPoint(span string) bool { return span == collectSpan }

// Settled is what a grant call or a drop's end came to, for the map to tell: the owner's inventory changes, and what became of each drop.
type Settled struct {
	Owner     Owner
	Inventory []model.InventoryChanged
	Loot      []model.Loot
}

// Result is a grant call's answer, which the map settles on its tick.
type Result struct {
	owner   kernel.CharacterID
	drops   []uint64 // the call's, in its order
	granted model.Granted
	err     error // the call failed as a whole, so each drop may or may not be granted
}

// Collect sends each owner's due drops to the item module, one call per owner at a time, and takes away the drops whose life ended waiting for room; it returns what to tell their owners.
func (g *Ground) Collect() []Settled {
	lost := g.expire()
	g.send()
	g.forgetIdleTurns()
	return lost
}

// expire loses the drops past their life still waiting for room; any other, not in a call, has one last try under its key.
func (g *Ground) expire() []Settled {
	var out []Settled
	g.drops = slices.DeleteFunc(g.drops, func(d *drop) bool {
		if d.expiresAt > g.tick || d.state == sent {
			return false
		}
		if d.state != noRoom {
			d.last = true
			return false
		}
		o := g.owners[d.owner]
		out = append(out, Settled{Owner: o.Owner, Loot: []model.Loot{g.lose(d)}})
		o.drops--
		g.release(d.owner)
		return true
	})
	return out
}

// call is the drops an owner's next grant call carries, and whether one of them makes it go now.
type call struct {
	owner kernel.CharacterID
	drops []*drop
	due   bool
}

// send hands each owner with no call in flight one for their ready drops, oldest first; when the pool's queue is full, they wait for the next tick.
func (g *Ground) send() {
	var calls []*call
	for _, d := range g.drops {
		o := g.owners[d.owner]
		if o.busy || d.state == sent || d.state == fresh && d.collectAt > g.tick {
			continue
		}
		k := slices.IndexFunc(calls, func(c *call) bool { return c.owner == d.owner })
		if k < 0 {
			calls = append(calls, &call{owner: d.owner})
			k = len(calls) - 1
		}
		c := calls[k]
		if len(c.drops) < g.tuning.BatchMax {
			c.drops = append(c.drops, d)
		}
		c.due = c.due || d.state == fresh || d.last || g.tick >= o.retryAt
	}
	for _, c := range calls {
		if c.due && !g.submit(c) {
			return
		}
	}
}

// submit hands a worker the owner's call, false when the pool's queue is full.
func (g *Ground) submit(c *call) bool {
	o := g.owners[c.owner]
	in := model.GrantCall{Account: o.Account, Character: o.ID, Drops: make([]model.Grant, 0, len(c.drops))}
	ids := make([]uint64, 0, len(c.drops))
	for _, d := range c.drops {
		in.Drops = append(in.Drops, model.Grant{Key: d.key, Template: d.template, Quantity: d.quantity, Stats: d.stats})
		ids = append(ids, d.id)
	}
	if !g.deps.ItemCalls.Submit(func(ctx context.Context) { g.deps.Settle(ctx, g.grant(ctx, c.owner, ids, in)) }) {
		return false
	}
	g.deps.Observer.ObserveBatch(len(ids))
	o.busy = true
	for _, d := range c.drops {
		d.state = sent
	}
	return true
}

// grant asks the item module for the call's drops, on a worker, retrying a transient failure once; its span is a consumer's, of the job a tick handed over.
func (g *Ground) grant(ctx context.Context, owner kernel.CharacterID, drops []uint64, in model.GrantCall) Result {
	ctx, span := g.tracer.Start(ctx, collectSpan, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
		attribute.String("character_id", in.Character.String()), attribute.Int("drops", len(in.Drops))))
	defer span.End()
	got, err := g.addMany(ctx, in)
	if err != nil && g.deps.Items.Retryable(err) {
		got, err = g.addMany(ctx, in) // each drop's key makes a retry harmless
	}
	if err == nil && len(got.Outcomes) != len(in.Drops) {
		err = fmt.Errorf("the item module answered %d of %d drops", len(got.Outcomes), len(in.Drops))
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		g.deps.Logger.WarnContext(ctx, "a grant call failed; its drops are tried again under their keys", "character_id", in.Character, "drops", len(in.Drops), "err", err)
	}
	return Result{owner: owner, drops: drops, granted: got, err: err}
}

func (g *Ground) addMany(ctx context.Context, in model.GrantCall) (model.Granted, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return g.deps.Items.AddMany(ctx, in)
}

// Settle applies a grant call's answer on the tick: granted drops leave for their owner, refused ones wait for room or are lost, and those of a failed call are tried again under their keys.
func (g *Ground) Settle(r Result) Settled {
	o := g.owners[r.owner]
	o.busy = false
	out := Settled{Owner: o.Owner}
	retry := false
	gone := map[uint64]bool{}
	for k, id := range r.drops {
		d := g.drops[slices.IndexFunc(g.drops, func(d *drop) bool { return d.id == id })]
		var answer model.GrantOutcome
		if r.err == nil {
			answer = r.granted.Outcomes[k]
		}
		switch {
		case r.err != nil && !d.last:
			d.state, d.last, retry = unsettled, d.expiresAt <= g.tick, true
		case answer.Kind == model.GrantAdded || answer.Kind == model.GrantReplayed:
			if s := answer.Replayed; len(s.Slots) > 0 {
				out.Inventory = append(out.Inventory, model.InventoryChanged{Tick: g.tick, Revision: s.Revision, Slots: s.Slots})
			}
			g.collected = append(g.collected, d.id)
			out.Loot = append(out.Loot, g.loot(d, model.LootCollected))
			gone[id] = true
		case answer.Kind == model.GrantNoRoom && d.expiresAt > g.tick:
			d.state, retry = noRoom, true
			if !d.told {
				d.told = true
				out.Loot = append(out.Loot, g.loot(d, model.LootWaiting))
			}
		default:
			if answer.Kind == model.GrantRefused {
				g.deps.Logger.Error("a drop was refused by a rule no retry passes; it is lost", "character_id", d.owner, "template", d.template, "reason", answer.Reason)
			}
			out.Loot = append(out.Loot, g.lose(d))
			gone[id] = true
		}
	}
	if r.err == nil && len(r.granted.Added.Slots) > 0 {
		out.Inventory = append(out.Inventory, model.InventoryChanged{Tick: g.tick, Revision: r.granted.Added.Revision, Slots: r.granted.Added.Slots})
	}
	if retry {
		o.retryAt = g.tick + g.roomCheck
	}
	g.drops = slices.DeleteFunc(g.drops, func(d *drop) bool { return gone[d.id] })
	o.drops -= len(gone)
	g.release(r.owner)
	return out
}

// lose takes a drop off the ground as gone, never collected.
func (g *Ground) lose(d *drop) model.Loot {
	g.removed = append(g.removed, d.id)
	return g.loot(d, model.LootLost)
}

func (g *Ground) loot(d *drop, kind model.LootKind) model.Loot {
	return model.Loot{
		Tick: g.tick, Kind: kind, MapID: g.deps.MapID, DropID: d.id,
		Owner: d.owner, OwnerName: g.owners[d.owner].Name, Template: d.template, Quantity: d.quantity,
	}
}

// release forgets an owner with no drop left on the ground and no call in flight.
func (g *Ground) release(id kernel.CharacterID) {
	if o := g.owners[id]; o.drops == 0 && !o.busy {
		delete(g.owners, id)
	}
}
