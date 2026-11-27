package loot

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/transient"
)

const (
	callFor    = 3500 * time.Millisecond // a grant call, its retry included: inside the 5 s the server gives a take, so each pick is answered
	reachSlack = 0.5                     // metres past reach a take may come from, for the client's lead
	retryEvery = 2 * time.Second         // items whose call got no answer are asked again this often
)

const tracerName = "finga/internal/application/world/loot"

// takeSpan names a grant call's trace: the job a tick hands to a worker.
const takeSpan = "world.take"

// EntryPoint reports whether a root span of this name starts a trace that is kept: a grant call's.
func EntryPoint(span string) bool { return span == takeSpan }

// Settled is what a grant call or a pouch's end came to, for the map to tell: the owner's inventory changes, and what became of the items.
type Settled struct {
	Owner     Owner
	Inventory []model.InventoryChanged
	Loot      []model.Loot
}

// Point is where a character's middle is.
type Point struct{ X, Y, Z float64 }

// picked is an item a call carries, and its pouch.
type picked struct {
	pouch *pouch
	entry *entry
}

// ask is one take: what each of its picks came to, in its order, and how it hears them once the last settles.
type ask struct {
	answers []model.PickAnswer
	at      map[*entry]int // where each pick in a call stands in answers
	answer  func([]model.PickAnswer)
}

// take is the items a call carries and the asks it answers.
type take struct {
	items []picked
	asks  []*ask
}

// Result is a grant call's answer, which the map settles on its tick.
type Result struct {
	owner   kernel.CharacterID
	items   []picked // the call's, in its order
	granted model.Granted
	err     error // the call failed as a whole, so each item may or may not be granted
}

// Take asks for the owner's picks: a pick of a pouch gone or not theirs, or of an item taken or named twice, is refused as gone, one of an item being taken as busy, and one of a pouch out of reach of their middle as out of reach. The rest go in a grant call at once, or wait for the call in flight; a third take while one waits is refused as busy, unless it only picks from the pouches the waiting one does, which it joins. answer runs on the tick once every pick is settled.
func (g *Ground) Take(o Owner, at Point, picks []model.Pick, answer func([]model.PickAnswer)) {
	a := &ask{answers: make([]model.PickAnswer, len(picks)), at: map[*entry]int{}, answer: answer}
	var items []picked
	named := map[*entry]bool{} // every item a pick names, taken or refused, so naming one again is refused as gone
	for k, pk := range picks {
		a.answers[k].Pick = pk
		p := g.find(pk.Pouch)
		var e *entry
		if p != nil && p.owner == o.ID && !p.gone {
			e = p.entry(pk.Item)
		}
		switch {
		case e == nil || named[e]:
			a.answers[k].Err = model.ErrPouchGone
		case e.taking:
			a.answers[k].Err = model.ErrTakeBusy
		case distance(at, p) > g.rules.Reach+reachSlack:
			a.answers[k].Err = model.ErrPouchOutOfReach
		case len(items) == g.tuning.BatchMax:
			a.answers[k].Err = model.ErrTakeBusy // more than one call carries: the rest come with the next take
		default:
			a.at[e] = k
			items = append(items, picked{p, e})
		}
		if e != nil {
			named[e] = true
		}
	}
	st := g.owners[o.ID]
	if len(items) == 0 || st == nil {
		answer(a.answers)
		return
	}
	st.Owner = o
	switch {
	case !st.busy:
		g.send(st, &take{items: items, asks: []*ask{a}})
	case st.waiting == nil:
		st.waiting = &take{items: items, asks: []*ask{a}}
		mark(items, true)
	case st.waiting.joins(items, g.tuning.BatchMax):
		st.waiting.items = append(st.waiting.items, items...)
		st.waiting.asks = append(st.waiting.asks, a)
		mark(items, true)
	default:
		for _, it := range items {
			a.answers[a.at[it.entry]].Err = model.ErrTakeBusy
		}
		answer(a.answers)
	}
}

// joins reports whether items may join the waiting take: all from its pouches, within one call.
func (t *take) joins(items []picked, most int) bool {
	if len(t.items)+len(items) > most {
		return false
	}
	return !slices.ContainsFunc(items, func(it picked) bool {
		return !slices.ContainsFunc(t.items, func(w picked) bool { return w.pouch == it.pouch })
	})
}

func mark(items []picked, taking bool) {
	for _, it := range items {
		it.entry.taking = taking
	}
}

func distance(at Point, p *pouch) float64 {
	dx, dy, dz := p.x-at.X, p.y-at.Y, p.z-at.Z
	return math.Sqrt(float64(dx*dx) + float64(dy*dy) + float64(dz*dz))
}

// send hands a worker the owner's call; when the pool's queue is full, its items go back, its takes hear they can try again, and items whose call got no answer wait the interval again.
func (g *Ground) send(o *owner, t *take) {
	in := model.GrantCall{Account: o.Account, Character: o.ID, Drops: make([]model.Grant, 0, len(t.items))}
	for _, it := range t.items {
		e := it.entry
		in.Drops = append(in.Drops, model.Grant{Key: e.key, Template: e.template, Quantity: e.quantity, Stats: copied(e.stats)})
	}
	id, items := o.ID, t.items // the worker reads only copies: the tick goes on rewriting the owner as their pouches fall
	if !g.deps.ItemCalls.Submit(func(ctx context.Context) { g.deps.Settle(ctx, g.grant(ctx, id, items, in)) }) {
		busy := fmt.Errorf("%w: every item-call worker is busy", transient.ErrUnavailable)
		o.retryAt = g.tick + g.retry
		for _, it := range t.items {
			it.entry.taking = it.entry.unsettled // one whose call got no answer stays taken, for its next try
		}
		for _, a := range t.asks {
			for _, it := range t.items {
				if k, ok := a.at[it.entry]; ok {
					a.answers[k].Err = busy
				}
			}
			a.answer(a.answers)
		}
		return
	}
	g.deps.Observer.ObserveBatch(len(t.items))
	mark(t.items, true)
	o.busy, o.asking = true, t.asks
}

// copied is a roll of its own for a worker's call, which shares no memory with the ground.
func copied(s *progression.ItemStats) *progression.ItemStats {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// grant asks the item module for the call's items, on a worker, retrying a transient failure once, both tries within callFor; its span is a consumer's, of the job a tick handed over.
func (g *Ground) grant(ctx context.Context, owner kernel.CharacterID, items []picked, in model.GrantCall) Result {
	ctx, span := g.tracer.Start(ctx, takeSpan, trace.WithNewRoot(), trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(
		attribute.String("character_id", in.Character.String()), attribute.Int("items", len(in.Drops))))
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, callFor)
	defer cancel()
	got, err := g.deps.Items.AddMany(ctx, in)
	if err != nil && ctx.Err() == nil && g.deps.Items.Retryable(err) {
		got, err = g.deps.Items.AddMany(ctx, in) // each item's key makes a retry harmless
	}
	if err == nil && len(got.Outcomes) != len(in.Drops) {
		err = fmt.Errorf("the item module answered %d of %d items", len(got.Outcomes), len(in.Drops))
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		g.deps.Logger.WarnContext(ctx, "a grant call failed; its items are asked again under their keys", "character_id", in.Character, "items", len(in.Drops), "err", err)
	}
	return Result{owner: owner, items: items, granted: got, err: err}
}

// Settle applies a grant call's answer on the tick: granted items leave their pouch for the owner's inventory, one refused for room stays, and those of a call that got no answer stay taken, asked again under their keys. Its takes hear each pick, an emptied pouch is gone at once, and a take waiting goes next.
func (g *Ground) Settle(r Result) Settled {
	o := g.owners[r.owner]
	o.busy = false
	out := Settled{Owner: o.Owner}
	var touched []*pouch
	taken := map[*pouch][]model.LootItem{}
	left := map[*pouch][]model.LootItem{}
	unsettled := false
	for k, it := range r.items {
		p, e := it.pouch, it.entry
		var answer model.GrantOutcome
		if r.err == nil {
			answer = r.granted.Outcomes[k]
		}
		if !slices.Contains(touched, p) {
			touched = append(touched, p)
		}
		item := model.LootItem{Template: e.template, Quantity: e.quantity}
		var err error
		switch {
		case r.err != nil && (!p.gone || !e.unsettled):
			e.unsettled, unsettled = true, true
			err = fmt.Errorf("%w: no answer from the item module; it is asked again", transient.ErrUnavailable)
		case r.err != nil:
			g.deps.Logger.Error("an item's grant got no answer past its pouch's life; it is given up, granted or not: its key keeps it from being granted twice", "character_id", p.owner, "template", e.template, "key", e.key)
			p.drop(e)
			err = model.ErrPouchGone
		case answer.Kind == model.GrantAdded || answer.Kind == model.GrantReplayed:
			if s := answer.Replayed; len(s.Slots) > 0 {
				out.Inventory = append(out.Inventory, model.InventoryChanged{Tick: g.tick, Revision: s.Revision, Slots: s.Slots})
			}
			p.drop(e)
			taken[p] = append(taken[p], item)
		case answer.Kind == model.GrantNoRoom:
			e.taking, e.unsettled = false, false
			err = g.tabFull(e.template)
			if p.gone { // nothing to wait in: it is left
				p.drop(e)
				left[p] = append(left[p], item)
			}
		default:
			g.deps.Logger.Error("an item was refused by a rule no retry passes; it is given up", "character_id", p.owner, "template", e.template, "reason", answer.Reason)
			p.drop(e)
			err = model.ErrPouchGone
		}
		for _, a := range o.asking {
			if at, ok := a.at[e]; ok {
				a.answers[at].Err = err
			}
		}
	}
	if r.err == nil && len(r.granted.Added.Slots) > 0 {
		out.Inventory = append(out.Inventory, model.InventoryChanged{Tick: g.tick, Revision: r.granted.Added.Revision, Slots: r.granted.Added.Slots})
	}
	for _, p := range touched {
		if items := taken[p]; len(items) > 0 {
			out.Loot = append(out.Loot, g.loot(p, model.LootTaken, items))
		}
		if items := left[p]; len(items) > 0 {
			out.Loot = append(out.Loot, g.loot(p, model.LootLeft, items))
		}
		if !p.gone && len(p.items) > 0 {
			g.changed(p)
		}
	}
	if unsettled {
		o.retryAt = g.tick + g.retry
	}
	if o.unsettled() {
		g.retrying[o.ID] = o
	} else {
		delete(g.retrying, o.ID)
	}
	asking := o.asking
	o.asking = nil
	for _, a := range asking {
		a.answer(a.answers)
	}
	if t := o.waiting; t != nil {
		o.waiting = nil
		g.send(o, t)
	}
	g.sweep()
	return out
}

// retryUnsettled asks again, under their keys, for the items of owners with no call in flight whose last call got no answer, once their interval passed.
func (g *Ground) retryUnsettled() {
	for id, o := range g.retrying {
		if o.busy || o.waiting != nil || g.tick < o.retryAt {
			continue
		}
		var items []picked
		for _, p := range o.pouches {
			for _, e := range p.items {
				if e.unsettled && len(items) < g.tuning.BatchMax {
					items = append(items, picked{p, e})
				}
			}
		}
		if len(items) == 0 {
			delete(g.retrying, id)
			continue
		}
		g.send(o, &take{items: items})
	}
}

// unsettled reports whether any of the owner's items had a call with no answer.
func (o *owner) unsettled() bool {
	return slices.ContainsFunc(o.pouches, func(p *pouch) bool {
		return slices.ContainsFunc(p.items, func(e *entry) bool { return e.unsettled })
	})
}

// tabFull is the refusal of an item whose tab has no free slot.
func (g *Ground) tabFull(template string) error {
	tmpl, _ := g.deps.Data.Item(template)
	switch tmpl.Tab {
	case gamedata.Equip:
		return kernel.ErrEquipTabFull
	case gamedata.Use:
		return kernel.ErrUseTabFull
	default:
		return kernel.ErrEtcTabFull
	}
}

func (p *pouch) drop(e *entry) {
	p.items = slices.DeleteFunc(p.items, func(o *entry) bool { return o == e })
}
