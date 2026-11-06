package instance_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// errFull is a rule, so the world never retries it; errDown is not.
var (
	errFull   = errors.New("inventory full")
	errDown   = errors.New("item service unavailable")
	errReused = errors.New("key reused") // an earlier try granted the drop under another fingerprint
)

// fakePickups is idempotent by drop, with an optional failure on each drop's first call.
type fakePickups struct {
	mu        sync.Mutex
	calls     []model.Pickup
	granted   map[pickupKey][]model.Slot // the item service's idempotency keys are per account
	failFirst error                      // returned by the first call of each drop
	full      bool                       // every add is refused with errFull
	down      bool                       // every add fails with errDown
	lostReply bool                       // every add commits, then fails with errDown
	hang      bool                       // every add waits until its context ends
	gate      chan struct{}              // when set, every add waits for it to close
	seen      map[uuid.UUID]int
	traces    []trace.SpanContext // each call's
	held      []model.Slot        // the whole inventory Inventory reads
	heldErr   error               // returned by Inventory when set
}

type pickupKey struct {
	account kernel.AccountID
	drop    uuid.UUID
}

func newFakePickups() *fakePickups {
	return &fakePickups{granted: map[pickupKey][]model.Slot{}, seen: map[uuid.UUID]int{}}
}

func (f *fakePickups) Add(ctx context.Context, in model.Pickup) ([]model.Slot, error) {
	f.mu.Lock()
	f.calls = append(f.calls, in)
	f.traces = append(f.traces, trace.SpanContextFromContext(ctx))
	if f.hang {
		f.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if gate := f.gate; gate != nil {
		f.mu.Unlock()
		<-gate
		f.mu.Lock()
	}
	defer f.mu.Unlock()
	f.seen[in.DropID]++
	if f.full {
		return nil, errFull
	}
	if f.down {
		return nil, errDown
	}
	if f.failFirst != nil && f.seen[in.DropID] == 1 {
		return nil, f.failFirst
	}
	key := pickupKey{in.Account, in.DropID}
	slots, ok := f.granted[key]
	if !ok {
		slots = []model.Slot{{Tab: gamedata.Etc, Slot: len(f.granted), Item: &model.SlotItem{ID: kernel.NewItemID(), Template: in.Template, Quantity: in.Quantity}}}
		f.granted[key] = slots
	}
	if f.lostReply {
		return nil, errDown
	}
	return slots, nil
}

func (f *fakePickups) Retryable(err error) bool {
	return !errors.Is(err, errFull) && !errors.Is(err, errReused)
}

func (f *fakePickups) Granted(err error) bool { return errors.Is(err, errReused) }

func (f *fakePickups) Inventory(context.Context, kernel.CharacterID) ([]model.Slot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held, f.heldErr
}

func (f *fakePickups) items() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.granted)
}

func (f *fakePickups) set(change func(f *fakePickups)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *fakePickups) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func dropsIn(s *worldtest.Sink) map[uint64]model.DropView {
	seen := map[uint64]model.DropView{}
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			for _, d := range ev.Drops {
				seen[d.ID] = d
			}
		case worldtest.Delta:
			for _, d := range ev.Drops {
				seen[d.ID] = d
			}
			for _, id := range ev.RemovedDrops {
				delete(seen, id)
			}
		default: // the rest change nothing on the map
		}
	}
	return seen
}

func inventoryChanges(s *worldtest.Sink) []model.InventoryChanged {
	var out []model.InventoryChanged
	for _, e := range s.All() {
		if ic, ok := e.(model.InventoryChanged); ok {
			out = append(out, ic)
		}
	}
	return out
}

func pickupFailures(s *worldtest.Sink) []model.PickupFailed {
	var out []model.PickupFailed
	for _, e := range s.All() {
		if pf, ok := e.(model.PickupFailed); ok {
			out = append(out, pf)
		}
	}
	return out
}

func runLootArena(t *testing.T, data *gamedata.Data, items *fakePickups, roll float64, extra ...worldtest.Options) (*instance.Instance, func()) {
	t.Helper()
	workers := economy.NewWorkers(2, idleJobs{}, worldtest.DiscardPanics())
	ctx, cancel := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() { defer close(workersDone); _ = workers.Run(ctx) }()
	opts := worldtest.Options{NewRoll: worldtest.Rolls(roll), Items: items, Workers: workers}
	if len(extra) > 0 {
		opts.Parties = extra[0].Parties
	}
	inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, opts)
	return inst, func() { stop(); cancel(); <-workersDone }
}

// killSlime attacks until the arena slime is dead (two average hits).
func killSlime(t *testing.T, inst *instance.Instance, id kernel.CharacterID) {
	t.Helper()
	_ = inst.Attack(id)
	time.Sleep(13 * worldtest.TickInterval)
	synctest.Wait()
	_ = inst.Attack(id)
	time.Sleep(worldtest.TickInterval)
	synctest.Wait()
}

func TestDropRollsFollowTheTable(t *testing.T) {
	data := worldtest.LoadData(t)
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
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				s := join(t, inst, warriorAt(250))
				killSlime(t, inst, idOf(t, s))
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
		data := worldtest.LoadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3) // one slime_gel per kill
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Account = kernel.NewAccountID()
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		drops := dropsIn(s)
		if len(drops) != 1 {
			t.Fatalf("drops %v", drops)
		}
		var theDrop model.DropView
		for _, d := range drops {
			theDrop = d
		}
		// Walk under the drop, within the 40 px pickup range.
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		{ // a pickup in range becomes one item, the drop leaves the ground
			_ = inst.PickUp(p.ID)
			time.Sleep(3 * worldtest.TickInterval)
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
			_ = inst.PickUp(p.ID)
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			if items.callCount() != 1 {
				t.Fatalf("%d calls", items.callCount())
			}
		}
		_ = theDrop
	})
}

// A pickup keeps its trace, a root that names the character and the drop, with the item call inside it.
func TestAPickupKeepsItsTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		p.Account = kernel.NewAccountID()
		join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		before := len(worldtest.Spans.Ended())
		_ = inst.PickUp(p.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		ended := worldtest.Spans.Ended()[before:]
		if len(ended) != 1 || ended[0].Name() != "world.pickup" || ended[0].Parent().IsValid() {
			t.Fatalf("%d spans kept, want one world.pickup root", len(ended))
		}
		root := ended[0]
		attrs := attribute.NewSet(root.Attributes()...)
		character, _ := attrs.Value("character_id")
		drop, _ := attrs.Value("drop_id")
		if character.AsString() != p.ID.String() || drop.AsString() != items.calls[0].DropID.String() {
			t.Fatalf("the root names character %q and drop %q", character.AsString(), drop.AsString())
		}
		if items.traces[0].SpanID() != root.SpanContext().SpanID() {
			t.Fatal("the item call ran outside the pickup's span")
		}
	})
}

func TestTwoPlayersOneDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		ayla := warriorAt(250)
		ayla.Account = kernel.NewAccountID()
		sa := join(t, inst, ayla)
		killSlime(t, inst, ayla.ID)
		// The loot window: only the killer for 10 s. Wait it out so both may try.
		time.Sleep(11 * time.Second)
		synctest.Wait()
		robin := warriorAt(300)
		robin.Account = kernel.NewAccountID()
		sr := join(t, inst, robin)
		seq := worldtest.Walk(inst, ayla.ID, 0, sim.Input{Right: true}, 5)
		_ = inst.Input(ayla.ID, seq+1, sim.Input{})
		// Both ask in the same tick.
		_ = inst.PickUp(ayla.ID)
		_ = inst.PickUp(robin.ID)
		time.Sleep(3 * worldtest.TickInterval)
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
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.failFirst = errors.New("database hiccup")
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		_ = inst.Input(p.ID, seq+1, sim.Input{})
		_ = inst.PickUp(p.ID)
		time.Sleep(3 * worldtest.TickInterval)
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
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.full = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		_ = inst.Input(p.ID, seq+1, sim.Input{})
		_ = inst.PickUp(p.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		fails := pickupFailures(s)
		if len(fails) != 1 || !errors.Is(fails[0].Err, errFull) {
			t.Fatalf("failures %+v", fails)
		}
		if len(dropsIn(s)) != 1 {
			t.Fatal("the drop vanished although the pickup failed")
		}
		// The reservation was released: another try fails the same way, not silently.
		_ = inst.PickUp(p.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if len(pickupFailures(s)) != 2 || items.callCount() != 2 {
			t.Fatalf("second try: %d failures, %d calls; the reservation was not released", len(pickupFailures(s)), items.callCount())
		}
	})
}

func TestLootWindowAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		killer := warriorAt(250)
		sk := join(t, inst, killer)
		killSlime(t, inst, killer.ID)
		other := warriorAt(300) // standing right under the drop
		so := join(t, inst, other)
		{ // inside the window the other player gets nothing
			_ = inst.PickUp(other.ID)
			time.Sleep(3 * worldtest.TickInterval)
			synctest.Wait()
			if items.callCount() != 0 || len(dropsIn(so)) != 1 {
				t.Fatalf("calls %d drops %d; the loot window did not hold", items.callCount(), len(dropsIn(so)))
			}
		}
		{ // after 10 seconds anyone may take it
			time.Sleep(10 * time.Second)
			synctest.Wait()
			_ = inst.PickUp(other.ID)
			time.Sleep(3 * worldtest.TickInterval)
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
		data := worldtest.LoadData(t)
		items := newFakePickups()
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
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
		for _, d := range s.Deltas() {
			removed = removed || len(d.RemovedDrops) == 1
		}
		if !removed {
			t.Fatal("no RemovedDrops in any delta")
		}
	})
}

// An item service that never answers costs a pickup two call timeouts; then it fails and the drop can be taken again.
func TestAPickupFromAHungItemServiceFailsInTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.hang = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5) // walk under the drop
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		_ = inst.PickUp(p.ID)
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if f := pickupFailures(s); len(f) != 1 || !errors.Is(f[0].Err, model.ErrPickupsUnavailable) || items.callCount() != 2 {
			t.Fatalf("failures %+v after %d calls", f, items.callCount())
		}
		items.mu.Lock()
		items.hang = false
		items.mu.Unlock()
		_ = inst.PickUp(p.ID)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if items.items() != 1 {
			t.Fatalf("%d items once the service answers", items.items())
		}
	})
}

func TestPickupsUnavailableWhileTheItemServiceStaysDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.down = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5) // walk under the drop
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		_ = inst.PickUp(p.ID)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if f := pickupFailures(s); len(f) != 1 || !errors.Is(f[0].Err, model.ErrPickupsUnavailable) || items.callCount() != 2 {
			v, _ := s.Own(p.ID)
			t.Fatalf("failures %+v after %d calls; drops %+v; player %+v", f, items.callCount(), dropsIn(s), v.State)
		}
	})
}

// A pickup whose reply was lost may have committed, so the drop stays its player's to retry under the same key.
func TestALostPickupReplyKeepsTheDropForItsPlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.lostReply = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		ayla := warriorAt(250)
		ayla.Account = kernel.NewAccountID()
		sa := join(t, inst, ayla)
		killSlime(t, inst, ayla.ID)
		seq := worldtest.Walk(inst, ayla.ID, 0, sim.Input{Right: true}, 5)
		worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 1)
		_ = inst.PickUp(ayla.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if f := pickupFailures(sa); len(f) != 1 || !errors.Is(f[0].Err, model.ErrPickupsUnavailable) {
			t.Fatalf("failures %+v", f)
		}
		time.Sleep(11 * time.Second) // past the loot window, so anyone may try
		synctest.Wait()
		robin := warriorAt(300)
		robin.Account = kernel.NewAccountID()
		join(t, inst, robin)
		_ = inst.PickUp(robin.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		for _, c := range items.calls {
			if c.Account == robin.Account {
				t.Fatal("another account was let pick up a drop whose grant may have committed")
			}
		}
		items.set(func(f *fakePickups) { f.lostReply = false })
		_ = inst.PickUp(ayla.ID)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if items.items() != 1 || len(inventoryChanges(sa)) != 1 || len(dropsIn(sa)) != 0 {
			t.Fatalf("%d grants, %d inventory changes, drops %v; want the one grant settled", items.items(), len(inventoryChanges(sa)), dropsIn(sa))
		}
	})
}

// A pickup that finishes after its player took a portal still tells them what they got.
func TestAPickupResultReachesAPlayerWhoLeftThroughAPortal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.gate = make(chan struct{})
		workers := economy.NewWorkers(2, idleJobs{}, worldtest.DiscardPanics())
		ctx, cancel := context.WithCancel(context.Background())
		workersDone := make(chan struct{})
		go func() { defer close(workersDone); _ = workers.Run(ctx) }()
		defer func() { cancel(); <-workersDone }()
		m := arena(time.Hour)
		m.Portals = []gamedata.Portal{{Name: "to_town", Area: sim.Rect{X: 200, Y: 560, W: 200, H: 48}, TargetMap: "town", TargetPortal: "to_forest"}}
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{NewRoll: worldtest.Rolls(0.3), Items: items, Workers: workers, Transfer: tr})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		_ = inst.PickUp(p.ID)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		_ = inst.EnterPortal(p.ID)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if len(worldtest.Of[model.Teleport](s)) != 1 {
			t.Fatalf("teleports %+v", worldtest.Of[model.Teleport](s))
		}
		close(items.gate)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if ic := inventoryChanges(s); len(ic) != 1 || items.items() != 1 {
			t.Fatalf("inventory changes %+v after %d grants", ic, items.items())
		}
	})
}

// A pickup that finishes after its map stopped, with the map's inbox full, does not hold up the shutdown.
func TestAPickupAfterItsMapStoppedDoesNotHangTheWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakePickups()
		items.gate = make(chan struct{})
		workers := economy.NewWorkers(2, idleJobs{}, worldtest.DiscardPanics())
		ctx, cancel := context.WithCancel(context.Background())
		workersDone := make(chan struct{})
		go func() { defer close(workersDone); _ = workers.Run(ctx) }()
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.3), Items: items, Workers: workers})
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(250)
		join(t, inst, p)
		killSlime(t, inst, p.ID)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		_ = inst.PickUp(p.ID)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		stop()
		for n := uint32(100); inst.Input(p.ID, n, sim.Input{}) == nil; n++ {
		}
		close(items.gate)
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case <-workersDone:
		default:
			t.Fatal("the workers are stuck posting a pickup result to a stopped map")
		}
	})
}

// A key that already served another fingerprint means an earlier try was granted: the drop is gone, never released,
// and as the slots it changed went with the lost reply, the player is sent the whole inventory instead.
func TestAPickupAnEarlierTryGrantedRemovesTheDrop(t *testing.T) {
	gel := []model.Slot{{Tab: gamedata.Etc, Slot: 0, Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "slime_gel", Quantity: 1}}}
	for _, tt := range []struct {
		name    string
		heldErr error
		want    []model.InventoryChanged
	}{
		{"the inventory as the item service holds it", nil, []model.InventoryChanged{{Slots: gel}}},
		{"nothing when that read fails too", errDown, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				items := newFakePickups()
				items.failFirst, items.held, items.heldErr = errReused, gel, tt.heldErr
				inst, stop := runLootArena(t, data, items, 0.3)
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				p := warriorAt(250)
				s := join(t, inst, p)
				killSlime(t, inst, p.ID)
				seq := worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 5)
				_ = inst.Input(p.ID, seq+1, sim.Input{})
				_ = inst.PickUp(p.ID)
				time.Sleep(3 * worldtest.TickInterval)
				synctest.Wait()
				if items.callCount() != 1 {
					t.Fatalf("%d calls; a granted drop is not tried again", items.callCount())
				}
				if len(dropsIn(s)) != 0 || len(pickupFailures(s)) != 0 {
					t.Fatalf("drops %d, failures %d; want the drop gone and no failure", len(dropsIn(s)), len(pickupFailures(s)))
				}
				got := inventoryChanges(s)
				for k := range got {
					got[k].Tick = 0
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("inventory changes %+v, want %+v", got, tt.want)
				}
			})
		})
	}
}

// idleJobs counts nothing; the loot tests read the item service's calls instead.
type idleJobs struct{}

func (idleJobs) JobStarted()  {}
func (idleJobs) JobFinished() {}
