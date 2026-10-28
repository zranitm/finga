package world

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/model"
)

// Loot rules (page 5, OD-05 default).
const (
	lootWindow  = 10 * time.Second // only the killer (and party, step 15) may take it
	dropLife    = 60 * time.Second // then it vanishes
	pickupRange = 40.0             // px from the character's centre
	dropSpread  = 24.0             // px either side of the mob
	dropSpacing = 20.0             // px between several drops of one mob
)

// drop is an item on the ground, in memory only (TD-06). It becomes a real
// item when a pickup commits.
type drop struct {
	id         uint64    // the wire ID
	key        uuid.UUID // the idempotency key of its pickup
	template   string
	quantity   int
	x, y       float64
	killer     model.CharacterID
	windowEnd  uint64 // tick until which only the killer may take it
	expiresAt  uint64
	reservedBy model.CharacterID // zero when free
}

func (d *drop) view() DropView {
	return DropView{ID: d.id, Template: d.template, Quantity: d.quantity, X: d.x, Y: d.y}
}

// rollDrops rolls a mob's drop table (each entry independently) and puts
// the drops on the ground beside it.
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
			id: i.nextDrop, key: uuid.New(), template: entry.Item, quantity: qty, x: x, y: mb.y, killer: killer.ID,
			windowEnd: i.tick + i.ticksFor(lootWindow), expiresAt: i.tick + i.ticksFor(dropLife),
		}
		i.drops = append(i.drops, d)
		i.newDrops = append(i.newDrops, d.view())
		n++
	}
}

// expireDrops removes drops past their life.
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

// pickUp finds the nearest eligible drop in range, reserves it, and hands
// the economic action to a worker. The tick never waits on the database.
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
		if d.windowEnd > i.tick && d.killer != p.ID && (i.opts.Parties == nil || !i.opts.Parties.SameParty(d.killer, p.ID)) {
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
	if i.opts.Items == nil || i.opts.Workers == nil {
		p.private = append(p.private, PickupFailed{Tick: i.tick, Err: ErrPickupsUnavailable})
		return
	}
	best.reservedBy = p.ID
	job := pickupJob{drop: *best, account: p.Account, character: p.ID}
	if !i.opts.Workers.Submit(func(ctx context.Context) { i.runPickup(ctx, job) }) {
		best.reservedBy = model.CharacterID{}
		p.private = append(p.private, PickupFailed{Tick: i.tick, Err: ErrPickupBusy})
	}
}

type pickupJob struct {
	drop      drop
	account   model.AccountID
	character model.CharacterID
}

// runPickup is the worker's side: one transaction with the drop ID as the
// idempotency key, retried once on a transient failure, then the result
// goes back to the instance's inbox.
func (i *Instance) runPickup(ctx context.Context, job pickupJob) {
	in := Pickup{Account: job.account, Character: job.character, DropID: job.drop.key, Template: job.drop.template, Quantity: job.drop.quantity}
	slots, err := i.opts.Items.Add(ctx, in)
	if err != nil && i.opts.Items.Retryable(err) {
		slots, err = i.opts.Items.Add(ctx, in) // the key makes a retry harmless
	}
	i.inbox <- message{pickup: &pickupResult{dropID: job.drop.id, who: job.character, slots: slots, err: err}}
}

type pickupResult struct {
	dropID uint64
	who    model.CharacterID
	slots  []Slot
	err    error
}

// finishPickup applies a worker's result inside the tick: success removes
// the drop and tells the player what changed; failure releases the
// reservation and the drop stays.
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
			d.reservedBy = model.CharacterID{}
		}
		if p != nil {
			p.private = append(p.private, PickupFailed{Tick: i.tick, Err: r.err})
		}
		return
	}
	if d != nil {
		i.drops = removeDrop(i.drops, d.id)
		i.removedDrops = append(i.removedDrops, d.id)
	}
	if p != nil {
		p.private = append(p.private, InventoryChanged{Tick: i.tick, Slots: r.slots})
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

// dropsFor is what a joining player sees on the ground.
func (i *Instance) dropViews() []DropView {
	out := make([]DropView, 0, len(i.drops))
	for _, d := range i.drops {
		out = append(out, d.view())
	}
	return out
}

var _ = gamedata.Etc // the drop table's items are validated by gamedata
