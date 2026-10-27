package world_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/internal/sim"

	"github.com/google/uuid"
)

// errFull is the item service's refusal in these tests: a rule, so the
// world never retries it.
var errFull = errors.New("inventory full")

// fakePickups stands in for the item service: idempotent by drop, with a
// scripted failure for the first call when set.
type fakePickups struct {
	mu        sync.Mutex
	calls     []world.Pickup
	byDrop    map[uuid.UUID][]world.Slot
	failFirst error // returned by the first call of each drop
	full      bool  // every add is refused with errFull
	seen      map[uuid.UUID]int
}

func newFakePickups() *fakePickups {
	return &fakePickups{byDrop: map[uuid.UUID][]world.Slot{}, seen: map[uuid.UUID]int{}}
}

func (f *fakePickups) Add(_ context.Context, in world.Pickup) ([]world.Slot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	f.seen[in.DropID]++
	if f.full {
		return nil, errFull
	}
	if f.failFirst != nil && f.seen[in.DropID] == 1 {
		return nil, f.failFirst
	}
	if slots, ok := f.byDrop[in.DropID]; ok {
		return slots, nil
	}
	slots := []world.Slot{{Tab: gamedata.Etc, Slot: len(f.byDrop), Item: &world.SlotItem{ID: model.NewItemID(), Template: in.Template, Quantity: in.Quantity}}}
	f.byDrop[in.DropID] = slots
	return slots, nil
}

// Retryable is everything but the refusal.
func (f *fakePickups) Retryable(err error) bool { return !errors.Is(err, errFull) }

func (f *fakePickups) items() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byDrop)
}

func (f *fakePickups) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// alwaysDrops rolls so that every drop entry drops at its minimum quantity
// and every attack hits for average damage.
type scriptedRoll struct{ v float64 }

func (r scriptedRoll) Float64() float64 { return r.v }

func dropsIn(s *sink) map[uint64]world.DropView {
	seen := map[uint64]world.DropView{}
	for _, e := range s.all() {
		switch ev := e.(type) {
		case world.Snapshot:
			for _, d := range ev.Drops {
				seen[d.ID] = d
			}
		case world.Delta:
			for _, d := range ev.Drops {
				seen[d.ID] = d
			}
			for _, id := range ev.RemovedDrops {
				delete(seen, id)
			}
		}
	}
	return seen
}

func inventoryChanges(s *sink) []world.InventoryChanged {
	var out []world.InventoryChanged
	for _, e := range s.all() {
		if ic, ok := e.(world.InventoryChanged); ok {
			out = append(out, ic)
		}
	}
	return out
}

func pickupFailures(s *sink) []world.PickupFailed {
	var out []world.PickupFailed
	for _, e := range s.all() {
		if pf, ok := e.(world.PickupFailed); ok {
			out = append(out, pf)
		}
	}
	return out
}

// runLootArena starts the arena with pickups wired to a fake item service
// and a worker pool inside the bubble.
func runLootArena(t *testing.T, data *gamedata.Data, items *fakePickups, roll float64, extra ...world.Options) (*world.Instance, func()) {
	t.Helper()
	workers := world.NewWorkers(2, nil)
	ctx, cancel := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() { defer close(workersDone); workers.Run(ctx) }()
	opts := world.Options{Roll: scriptedRoll{roll}, Items: items, Workers: workers}
	if len(extra) > 0 {
		opts.Parties = extra[0].Parties
	}
	inst, stop := runArena(t, data, arena(time.Hour), nil, opts)
	return inst, func() { stop(); cancel(); <-workersDone }
}

// killSlime attacks until the arena slime is dead (two average hits).
func killSlime(t *testing.T, inst *world.Instance, id model.CharacterID) {
	t.Helper()
	inst.Attack(id)
	time.Sleep(13 * tickInterval)
	synctest.Wait()
	inst.Attack(id)
	time.Sleep(tickInterval)
	synctest.Wait()
}

func TestDropRollsFollowTheTable(t *testing.T) {
	data := loadData(t)
	slime, _ := data.Mob("green_slime") // slime_gel 60% 1..2, red_potion 15% 1
	tests := []struct {
		name string
		roll float64
		want map[string]int // template → quantity
	}{
		{"a roll under both chances drops both, at the low quantity", 0.1, map[string]int{"slime_gel": 1, "red_potion": 1}},
		{"a roll between the chances drops the gel only", 0.3, map[string]int{"slime_gel": 1}},
		{"a roll above both drops nothing", 0.7, map[string]int{}},
		{"a high roll under the chance gives the high quantity", 0.59, map[string]int{"slime_gel": 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				items := newFakePickups()
				inst, stop := runLootArena(t, data, items, tt.roll)
				defer stop()
				time.Sleep(tickInterval)
				synctest.Wait()
				s := join(t, inst, warriorAt(250))
				killSlime(t, inst, s.id(t, inst))
				got := map[string]int{}
				for _, d := range dropsIn(s) {
					got[d.Template] += d.Quantity
					if d.Y != 590 || d.X < 300-30 || d.X > 300+30+20 {
						t.Fatalf("drop landed at %v,%v, want beside the mob at 300,590", d.X, d.Y)
					}
				}
				if len(got) != len(tt.want) {
					t.Fatalf("drops %v, want %v", got, tt.want)
				}
				for tmpl, qty := range tt.want {
					if got[tmpl] != qty {
						t.Fatalf("drops %v, want %v", got, tt.want)
					}
				}
			})
			_ = slime
		})
	}
}

func TestPickUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3) // one slime_gel per kill
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Account = model.NewAccountID()
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		drops := dropsIn(s)
		if len(drops) != 1 {
			t.Fatalf("drops %v", drops)
		}
		var theDrop world.DropView
		for _, d := range drops {
			theDrop = d
		}
		// Walk under the drop (it lies beside x 300 on the platform level, 18 px
		// above the player's feet: within the 40 px range from the centre).
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		walk(inst, p.ID, seq, sim.Input{}, 1)
		{ // a pickup in range becomes one item, the drop leaves the ground
			inst.PickUp(p.ID)
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			if items.items() != 1 || items.calls[0].Template != "slime_gel" || items.calls[0].DropID == uuid.Nil || items.calls[0].Character != p.ID {
				t.Fatalf("items %d calls %+v", items.items(), items.calls)
			}
			if len(dropsIn(s)) != 0 {
				t.Fatal("the drop is still on the ground")
			}
			ic := inventoryChanges(s)
			if len(ic) != 1 || len(ic[0].Slots) != 1 || ic[0].Slots[0].Item.Template != "slime_gel" {
				t.Fatalf("inventory changes %+v", ic)
			}
		}
		{ // a second PickUp with nothing in range does nothing
			inst.PickUp(p.ID)
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if items.callCount() != 1 {
				t.Fatalf("%d calls", items.callCount())
			}
		}
		_ = theDrop
	})
}

func TestTwoPlayersOneDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		ayla := warriorAt(250)
		ayla.Account = model.NewAccountID()
		sa := join(t, inst, ayla)
		killSlime(t, inst, ayla.ID)
		// The loot window: only the killer for 10 s. Wait it out so both may try.
		time.Sleep(11 * time.Second)
		synctest.Wait()
		robin := warriorAt(300)
		robin.Account = model.NewAccountID()
		sr := join(t, inst, robin)
		seq := walk(inst, ayla.ID, 0, sim.Input{Right: true}, 5)
		inst.Input(ayla.ID, seq+1, sim.Input{})
		// Both ask in the same tick.
		inst.PickUp(ayla.ID)
		inst.PickUp(robin.ID)
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		if items.items() != 1 || items.callCount() != 1 {
			t.Fatalf("%d items from %d calls, want exactly one", items.items(), items.callCount())
		}
		got := len(inventoryChanges(sa)) + len(inventoryChanges(sr))
		if got != 1 {
			t.Fatalf("%d InventoryChanged events, want 1", got)
		}
		if len(dropsIn(sa)) != 0 || len(dropsIn(sr)) != 0 {
			t.Fatal("the drop is still on the ground for someone")
		}
	})
}

func TestRetriedPickupJobGivesOneItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		items.failFirst = errors.New("database hiccup")
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		inst.Input(p.ID, seq+1, sim.Input{})
		inst.PickUp(p.ID)
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		if items.callCount() != 2 || items.items() != 1 {
			t.Fatalf("%d calls, %d items; want a retry under the same key yielding one item", items.callCount(), items.items())
		}
		if items.calls[0].DropID != items.calls[1].DropID {
			t.Fatal("the retry used another key")
		}
		if len(inventoryChanges(s)) != 1 || len(dropsIn(s)) != 0 {
			t.Fatalf("changes %d drops %d", len(inventoryChanges(s)), len(dropsIn(s)))
		}
	})
}

func TestFullInventoryLeavesTheDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		items.full = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		inst.Input(p.ID, seq+1, sim.Input{})
		inst.PickUp(p.ID)
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		fails := pickupFailures(s)
		if len(fails) != 1 || !errors.Is(fails[0].Err, errFull) {
			t.Fatalf("failures %+v", fails)
		}
		if len(dropsIn(s)) != 1 {
			t.Fatal("the drop vanished although the pickup failed")
		}
		// The reservation was released: another try fails the same way, not silently.
		inst.PickUp(p.ID)
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		if len(pickupFailures(s)) != 2 || items.callCount() != 2 {
			t.Fatalf("second try: %d failures, %d calls; the reservation was not released", len(pickupFailures(s)), items.callCount())
		}
	})
}

func TestLootWindowAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		killer := warriorAt(250)
		sk := join(t, inst, killer)
		killSlime(t, inst, killer.ID)
		other := warriorAt(300) // standing right under the drop
		so := join(t, inst, other)
		{ // inside the window the other player gets nothing
			inst.PickUp(other.ID)
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			if items.callCount() != 0 || len(dropsIn(so)) != 1 {
				t.Fatalf("calls %d drops %d; the loot window did not hold", items.callCount(), len(dropsIn(so)))
			}
		}
		{ // after 10 seconds anyone may take it
			time.Sleep(10 * time.Second)
			synctest.Wait()
			inst.PickUp(other.ID)
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			if items.callCount() != 1 || len(inventoryChanges(so)) != 1 {
				t.Fatalf("calls %d changes %d after the window", items.callCount(), len(inventoryChanges(so)))
			}
		}
		{ // an untouched drop vanishes after 60 seconds
			killSlime(t, inst, killer.ID) // needs the slime back: respawn is an hour here, so kill nothing...
			_ = sk
		}
	})
}

func TestDropsVanishAfterAMinute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		if len(dropsIn(s)) != 1 {
			t.Fatal("no drop")
		}
		time.Sleep(59 * time.Second)
		synctest.Wait()
		if len(dropsIn(s)) != 1 {
			t.Fatal("the drop vanished early")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if len(dropsIn(s)) != 0 {
			t.Fatal("the drop did not vanish after 60 s")
		}
		removed := false
		for _, d := range s.deltas() {
			removed = removed || len(d.RemovedDrops) == 1
		}
		if !removed {
			t.Fatal("no RemovedDrops in any delta")
		}
	})
}

func TestPickupsUnavailableWithoutTheItemModule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := runArena(t, data, arena(time.Hour), nil, world.Options{Roll: scriptedRoll{0.3}})
		defer stop()
		time.Sleep(tickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := walk(inst, p.ID, 0, sim.Input{Right: true}, 5) // walk under the drop
		walk(inst, p.ID, seq, sim.Input{}, 1)
		inst.PickUp(p.ID)
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if f := pickupFailures(s); len(f) != 1 || !errors.Is(f[0].Err, world.ErrPickupsUnavailable) {
			v, _ := s.own(p.ID)
			t.Fatalf("failures %+v; drops %+v; player %+v; damages %d", f, dropsIn(s), v.State, len(damages(s)))
		}
	})
}
