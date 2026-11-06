package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/kernel"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/model"
)

const (
	lootWindow  = 10 * time.Second // only the killer and their party may take it
	dropLife    = 60 * time.Second
	pickupRange = 40.0 // px from the character's centre
	dropSpread  = 24.0 // px either side of the mob
	dropSpacing = 20.0 // px between several drops of one mob
)

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
}

func (d *drop) view() model.DropView {
	return model.DropView{ID: d.id, Template: d.template, Quantity: d.quantity, X: d.x, Y: d.y}
}

func (i *Instance) rollDrops(mb *mob, killer *player) {
	n := 0
	for _, entry := range mb.def.Drops {
		if i.roll.Float64() >= entry.Chance {
			continue
		}
		qty := entry.Min
		if entry.Max > entry.Min {
			qty += int(i.roll.Float64() * float64(entry.Max-entry.Min+1))
			qty = min(qty, entry.Max)
		}
		i.nextDrop++
		x := mb.x + (i.roll.Float64()*2-1)*dropSpread + float64(n)*dropSpacing
		d := &drop{
			id: i.nextDrop, key: uuidv7.New(), template: entry.Item, quantity: qty, x: x, y: mb.y, killer: killer.ID,
			windowEnd: i.tick + i.ticksFor(lootWindow), expiresAt: i.tick + i.ticksFor(dropLife),
		}
		i.drops = append(i.drops, d)
		i.newDrops = append(i.newDrops, d.view())
		n++
	}
}

func (i *Instance) expireDrops() {
	kept := i.drops[:0]
	for _, d := range i.drops {
		if d.expiresAt <= i.tick && (d.reservedBy.IsZero() || d.unsettled) {
			i.removedDrops = append(i.removedDrops, d.id)
			continue
		}
		kept = append(kept, d)
	}
	i.drops = kept
}

// pickUp reserves the nearest eligible drop and hands it to a worker.
func (i *Instance) pickUp(p *player) {
	if p.dead {
		return
	}
	cx, cy := p.state.X, p.state.Y-i.params.Height/2
	var best *drop
	bestDist := math.Inf(1)
	for _, d := range i.drops {
		if !d.reservedBy.IsZero() && (!d.unsettled || d.reservedBy != p.ID) {
			continue
		}
		if d.windowEnd > i.tick && d.killer != p.ID && !i.deps.Parties.SameParty(d.killer, p.ID) {
			continue // someone else's loot for now (the killer's party may)
		}
		dist := math.Hypot(d.x-cx, d.y-cy)
		if dist <= pickupRange && dist < bestDist {
			best, bestDist = d, dist
		}
	}
	if best == nil {
		return
	}
	unsettled := best.unsettled
	best.reservedBy, best.unsettled = p.ID, false
	job := pickupJob{drop: *best, account: p.Account, character: p.ID, sink: p.sink}
	if !i.deps.Workers.Submit(func(ctx context.Context) { i.runPickup(ctx, job) }) {
		if best.unsettled = unsettled; !unsettled {
			best.reservedBy = kernel.CharacterID{}
		}
		p.private = append(p.private, model.PickupFailed{Tick: i.tick, Err: model.ErrPickupBusy})
	}
}

const tracerName = "finga/services/world/internal/application"

// pickupSpan names a pickup's trace: the call a tick hands to a worker.
const pickupSpan = "world.pickup"

// EntryPoint reports whether a root span of this name starts a trace that is kept: a pickup's.
func EntryPoint(span string) bool { return span == pickupSpan }

type pickupJob struct {
	drop      drop
	account   kernel.AccountID
	character kernel.CharacterID
	sink      Sink // the player's, for a result that arrives after they left the map
}

// runPickup runs on a worker and posts the result back to the inbox.
func (i *Instance) runPickup(ctx context.Context, job pickupJob) {
	in := model.Pickup{Account: job.account, Character: job.character, DropID: job.drop.key, Template: job.drop.template, Quantity: job.drop.quantity}
	ctx, span := i.tracer.Start(ctx, pickupSpan, trace.WithNewRoot(), trace.WithAttributes(
		attribute.String("character_id", in.Character.String()), attribute.String("drop_id", in.DropID.String())))
	defer span.End()
	slots, err := i.addItem(ctx, in)
	if err != nil && i.deps.Items.Retryable(err) {
		slots, err = i.addItem(ctx, in) // the key makes a retry harmless
		if err != nil && i.deps.Items.Retryable(err) {
			err = fmt.Errorf("%w: %w", model.ErrPickupsUnavailable, err)
		}
	}
	if err != nil && i.deps.Items.Granted(err) {
		slots, err = nil, nil // granted, though the slots it changed went with the lost reply
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	select {
	case i.inbox <- &pickupResult{dropID: job.drop.id, who: job.character, sink: job.sink, slots: slots, err: err}:
	case <-ctx.Done(): // the map stopped first; its inbox may never drain
	}
}

func (i *Instance) addItem(ctx context.Context, in model.Pickup) ([]model.Slot, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return i.deps.Items.Add(ctx, in)
}

type pickupResult struct {
	dropID uint64
	who    kernel.CharacterID
	sink   Sink
	slots  []model.Slot
	err    error
}

func (i *Instance) finishPickup(r *pickupResult) {
	var d *drop
	for _, cand := range i.drops {
		if cand.id == r.dropID {
			d = cand
			break
		}
	}
	tell := func(e model.Event) {
		if p, ok := i.players[r.who]; ok {
			p.private = append(p.private, e)
		} else {
			r.sink.Send(e) // they left through a portal or a death since; the connection is the same
		}
	}
	if r.err != nil {
		if d != nil && d.reservedBy == r.who {
			if errors.Is(r.err, model.ErrPickupsUnavailable) {
				d.unsettled = true // the add may have committed before its reply was lost
			} else {
				d.reservedBy = kernel.CharacterID{}
			}
		}
		tell(model.PickupFailed{Tick: i.tick, Err: r.err})
		return
	}
	if d != nil {
		i.drops = removeDrop(i.drops, d.id)
		i.removedDrops = append(i.removedDrops, d.id)
	}
	if len(r.slots) > 0 {
		tell(model.InventoryChanged{Tick: i.tick, Slots: r.slots})
	}
}

func removeDrop(drops []*drop, id uint64) []*drop {
	for k, d := range drops {
		if d.id == id {
			return append(drops[:k], drops[k+1:]...)
		}
	}
	return drops
}

func (i *Instance) dropViews() []model.DropView {
	out := make([]model.DropView, 0, len(i.drops))
	for _, d := range i.drops {
		out = append(out, d.view())
	}
	return out
}
