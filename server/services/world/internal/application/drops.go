package application

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

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
		if d.expiresAt <= i.tick && d.reservedBy.IsZero() {
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
		if !d.reservedBy.IsZero() {
			continue
		}
		if d.windowEnd > i.tick && d.killer != p.ID && !i.opts.Parties.SameParty(d.killer, p.ID) {
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
	best.reservedBy = p.ID
	job := pickupJob{drop: *best, account: p.Account, character: p.ID}
	if !i.opts.Workers.Submit(func(ctx context.Context) { i.runPickup(ctx, job) }) {
		best.reservedBy = kernel.CharacterID{}
		p.private = append(p.private, model.PickupFailed{Tick: i.tick, Err: model.ErrPickupBusy})
	}
}

type pickupJob struct {
	drop      drop
	account   kernel.AccountID
	character kernel.CharacterID
}

// runPickup runs on a worker and posts the result back to the inbox.
func (i *Instance) runPickup(ctx context.Context, job pickupJob) {
	in := model.Pickup{Account: job.account, Character: job.character, DropID: job.drop.key, Template: job.drop.template, Quantity: job.drop.quantity}
	slots, err := i.opts.Items.Add(ctx, in)
	if err != nil && i.opts.Items.Retryable(err) {
		slots, err = i.opts.Items.Add(ctx, in) // the key makes a retry harmless
		if err != nil && i.opts.Items.Retryable(err) {
			err = fmt.Errorf("%w: %w", model.ErrPickupsUnavailable, err)
		}
	}
	i.inbox <- message{pickup: &pickupResult{dropID: job.drop.id, who: job.character, slots: slots, err: err}}
}

type pickupResult struct {
	dropID uint64
	who    kernel.CharacterID
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
	p := i.players[r.who]
	if r.err != nil {
		if d != nil && d.reservedBy == r.who {
			d.reservedBy = kernel.CharacterID{}
		}
		if p != nil {
			p.private = append(p.private, model.PickupFailed{Tick: i.tick, Err: r.err})
		}
		return
	}
	if d != nil {
		i.drops = removeDrop(i.drops, d.id)
		i.removedDrops = append(i.removedDrops, d.id)
	}
	if p != nil {
		p.private = append(p.private, model.InventoryChanged{Tick: i.tick, Slots: r.slots})
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
