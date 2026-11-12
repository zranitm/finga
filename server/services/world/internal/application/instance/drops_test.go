package instance_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

var errDown = errors.New("item service unavailable")

// fakeItems is the item service, idempotent by each drop's key per account, as the real one is.
type fakeItems struct {
	mu          sync.Mutex
	calls       []model.GrantCall
	granted     map[grantKey]model.Inventory // each key's stored reply
	revision    uint64
	full        bool          // every drop not granted before is refused for room
	down        bool          // every call fails
	lostReplies int           // this many calls commit, then fail as their reply is lost
	hang        bool          // every call waits until its context ends
	gate        chan struct{} // when set, every call waits for it to close
}

type grantKey struct {
	account kernel.AccountID
	key     uuid.UUID
}

func newFakeItems() *fakeItems { return &fakeItems{granted: map[grantKey]model.Inventory{}} }

func (f *fakeItems) AddMany(ctx context.Context, c model.GrantCall) (model.Granted, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	if f.hang {
		f.mu.Unlock()
		<-ctx.Done()
		return model.Granted{}, ctx.Err()
	}
	if gate := f.gate; gate != nil {
		f.mu.Unlock()
		<-gate
		f.mu.Lock()
	}
	defer f.mu.Unlock()
	if f.down {
		return model.Granted{}, errDown
	}
	var out model.Granted
	var added []grantKey
	for _, d := range c.Drops {
		k := grantKey{c.Account, d.Key}
		if reply, ok := f.granted[k]; ok {
			out.Outcomes = append(out.Outcomes, model.GrantOutcome{Kind: model.GrantReplayed, Replayed: reply})
			continue
		}
		if f.full {
			out.Outcomes = append(out.Outcomes, model.GrantOutcome{Kind: model.GrantNoRoom})
			continue
		}
		out.Outcomes = append(out.Outcomes, model.GrantOutcome{Kind: model.GrantAdded})
		slot := model.Slot{Tab: gamedata.Etc, Slot: len(f.granted), Item: &model.SlotItem{ID: kernel.NewItemID(), Template: d.Template, Quantity: d.Quantity}}
		out.Added.Slots = append(out.Added.Slots, slot)
		f.granted[k] = model.Inventory{Slots: []model.Slot{slot}}
		added = append(added, k)
	}
	if len(added) > 0 {
		f.revision++
		out.Added.Revision = f.revision
		for _, k := range added {
			f.granted[k] = model.Inventory{Revision: f.revision, Slots: f.granted[k].Slots}
		}
	}
	if f.lostReplies > 0 {
		f.lostReplies--
		return model.Granted{}, errDown
	}
	return out, nil
}

// Retryable is any failure: the fake has no rule that fails a whole call.
func (f *fakeItems) Retryable(error) bool { return true }

func (f *fakeItems) set(change func(f *fakeItems)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *fakeItems) made() []model.GrantCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// items is how many drops were granted, each once.
func (f *fakeItems) items() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.granted)
}

// dropsIn are the drops on the ground as s saw them.
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
			for _, id := range slices.Concat(ev.RemovedDrops, ev.CollectedDrops) {
				delete(seen, id)
			}
		default: // the rest change nothing on the map
		}
	}
	return seen
}

// collected are the drops s saw leave for an inventory.
func collected(s *worldtest.Sink) []uint64 {
	var out []uint64
	for _, d := range s.Deltas() {
		out = append(out, d.CollectedDrops...)
	}
	return out
}

func removedDrops(s *worldtest.Sink) []uint64 {
	var out []uint64
	for _, d := range s.Deltas() {
		out = append(out, d.RemovedDrops...)
	}
	return out
}

func inventoryChanges(s *worldtest.Sink) []model.InventoryChanged {
	return worldtest.Of[model.InventoryChanged](s)
}

func lootOf(s *worldtest.Sink) []model.Loot { return worldtest.Of[model.Loot](s) }

func runLootArena(t *testing.T, data *gamedata.Data, items *fakeItems, roll float64, extra ...worldtest.Options) (*instance.Instance, func()) {
	t.Helper()
	workers := itemcalls.NewPool(itemcalls.Deps{Observer: idleJobs{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2})
	ctx, cancel := context.WithCancel(context.Background())
	workersDone := make(chan struct{})
	go func() { defer close(workersDone); _ = workers.Run(ctx) }()
	opts := worldtest.Options{NewRoll: worldtest.Rolls(roll), Items: items, ItemCalls: workers}
	if len(extra) > 0 {
		opts.Parties, opts.Transfer = extra[0].Parties, extra[0].Transfer
	}
	m := arena(time.Hour)
	m.Portals = []gamedata.Portal{{Name: "to_town", Area: sim.Rect{X: 200, Y: 560, W: 60, H: 48}, TargetMap: "town", TargetPortal: "to_forest"}}
	inst, stop := worldtest.RunMap(t, data, m, nil, opts)
	return inst, func() { stop(); cancel(); <-workersDone }
}

// killSlime attacks until the arena slime is dead (two average hits), the second as the warrior's 900 ms attack ends.
func killSlime(t *testing.T, inst *instance.Instance, id kernel.CharacterID) {
	t.Helper()
	_ = inst.Attack(id, 0)
	time.Sleep(18 * worldtest.TickInterval)
	synctest.Wait()
	_ = inst.Attack(id, 0)
	time.Sleep(worldtest.TickInterval)
	synctest.Wait()
}

// settle waits out d and lets the workers' answers land.
func settle(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestDropRollsFollowTheTable(t *testing.T) {
	data := worldtest.LoadData(t)
	tests := []struct {
		name string
		roll float64
		want map[string]int // template → quantity
	}{ // the slime drops slime_gel 90% 1..2, red_potion 22.5% 1
		{"a roll under both chances drops both, at the low quantity", 0.1, map[string]int{"slime_gel": 1, "red_potion": 1}},
		{"a roll between the chances drops the gel only", 0.3, map[string]int{"slime_gel": 1}},
		{"a roll above both drops nothing", 0.95, map[string]int{}},
		{"a high roll under the chance gives the high quantity", 0.89, map[string]int{"slime_gel": 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := runLootArena(t, data, newFakeItems(), tt.roll)
				defer stop()
				settle(worldtest.TickInterval)
				s := join(t, inst, warriorAt(250))
				killer := idOf(t, s)
				killSlime(t, inst, killer)
				got := map[string]int{}
				for _, d := range dropsIn(s) {
					got[d.Template] += d.Quantity
					if d.Y != 590 || d.X < 300-30 || d.X > 300+30+20 {
						t.Fatalf("drop landed at %v,%v, want beside the mob at 300,590", d.X, d.Y)
					}
					if d.Owner != killer {
						t.Fatalf("drop owned by %v, want the killer alone", d.Owner)
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
		})
	}
}

// With no key pressed, a drop flies to its killer a second after the kill: one item, and the map sees it collected.
func TestADropFliesToItsKiller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		inst, stop := runLootArena(t, data, items, 0.3) // one slime_gel per kill
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		p.Account = kernel.NewAccountID()
		s := join(t, inst, p)
		other := join(t, inst, warriorAt(100))
		killSlime(t, inst, p.ID)
		drops := dropsIn(s)
		if len(drops) != 1 || len(items.made()) != 0 {
			t.Fatalf("drops %v after %d calls; want the gel on the ground for now", drops, len(items.made()))
		}
		var gel model.DropView
		for _, d := range drops {
			gel = d
		}
		settle(time.Second + 2*worldtest.TickInterval)
		calls := items.made()
		if len(calls) != 1 || calls[0].Account != p.Account || calls[0].Character != p.ID || len(calls[0].Drops) != 1 || calls[0].Drops[0].Template != "slime_gel" {
			t.Fatalf("calls %+v; want the gel granted to its killer", calls)
		}
		for _, sink := range []*worldtest.Sink{s, other} {
			if len(dropsIn(sink)) != 0 || !slices.Equal(collected(sink), []uint64{gel.ID}) || len(removedDrops(sink)) != 0 {
				t.Fatalf("drops %v, collected %v, removed %v; want the gel seen collected", dropsIn(sink), collected(sink), removedDrops(sink))
			}
		}
		if ic := inventoryChanges(s); len(ic) != 1 || ic[0].Revision != 1 || ic[0].Slots[0].Item.Template != "slime_gel" {
			t.Fatalf("inventory changes %+v", ic)
		}
		if l := lootOf(s); len(l) != 1 || l[0].Kind != model.LootCollected || l[0].DropID != gel.ID || l[0].Owner != p.ID || l[0].OwnerName != "Ayla" || l[0].MapID != "arena" {
			t.Fatalf("loot %+v", l)
		}
		if len(lootOf(other)) != 0 || len(inventoryChanges(other)) != 0 {
			t.Fatal("a player outside the party heard of the killer's loot")
		}
	})
}

// A party's drops go in turn to its members alive on the map, and each member there hears what the others got.
func TestAPartyOnTheMapTakesItsDropsInTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		killer, mate, dead := warriorAt(250), warriorAt(150), warriorAt(300)
		dead.HP = 1 // standing in the slime: dies on the first tick
		away := kernel.NewCharacterID()
		parties := &fakeParties{parties: [][]kernel.CharacterID{{dead.ID, away, killer.ID, mate.ID}}}
		inst, stop := runLootArena(t, data, items, 0.1, worldtest.Options{Parties: parties}) // a gel and a potion per kill
		defer stop()
		settle(worldtest.TickInterval)
		sk, sm := join(t, inst, killer), join(t, inst, mate)
		join(t, inst, dead)
		stranger := join(t, inst, warriorAt(120))
		killSlime(t, inst, killer.ID)
		owners := map[string]kernel.CharacterID{}
		for _, d := range dropsIn(sk) {
			owners[d.Template] = d.Owner
		}
		if owners["slime_gel"] != killer.ID || owners["red_potion"] != mate.ID {
			t.Fatalf("owners %v; want the gel the killer's and the potion the mate's, the dead and the absent skipped", owners)
		}
		settle(time.Second + 2*worldtest.TickInterval)
		for _, s := range []*worldtest.Sink{sk, sm} {
			got := map[kernel.CharacterID]string{}
			for _, l := range lootOf(s) {
				if l.Kind == model.LootCollected {
					got[l.Owner] = l.Template
				}
			}
			if len(got) != 2 || got[killer.ID] != "slime_gel" || got[mate.ID] != "red_potion" {
				t.Fatalf("collected %v; want both members to hear both drops", got)
			}
		}
		if len(inventoryChanges(sk)) != 1 || len(inventoryChanges(sm)) != 1 || len(lootOf(stranger)) != 0 {
			t.Fatal("an inventory change went to the wrong player, or a stranger heard the party's loot")
		}
	})
}

// A full inventory keeps the drop on the ground, owned: its owner hears once, and gets it once there is room.
func TestADropWaitsForRoom(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.full = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		settle(5 * time.Second)
		if l := lootOf(s); len(l) != 1 || l[0].Kind != model.LootWaiting || len(dropsIn(s)) != 1 {
			t.Fatalf("loot %+v, drops %v; want the gel waiting, told once", l, dropsIn(s))
		}
		if n := len(items.made()); n < 2 || n > 3 {
			t.Fatalf("%d calls in 5 s; want one per 2 s room check", n)
		}
		items.set(func(f *fakeItems) { f.full = false })
		settle(2*time.Second + 2*worldtest.TickInterval)
		if l := lootOf(s); len(l) != 2 || l[1].Kind != model.LootCollected || len(dropsIn(s)) != 0 || items.items() != 1 {
			t.Fatalf("loot %+v, drops %v; want the gel collected once there was room", l, dropsIn(s))
		}
	})
}

// A drop still waiting at the end of its minute is lost: it leaves the ground as gone, and its owner hears.
func TestADropWaitingAMinuteIsLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.full = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		settle(58 * time.Second)
		if len(dropsIn(s)) != 1 {
			t.Fatal("the drop left early")
		}
		settle(3 * time.Second)
		l := lootOf(s)
		if len(dropsIn(s)) != 0 || len(removedDrops(s)) != 1 || len(collected(s)) != 0 || len(l) != 2 || l[1].Kind != model.LootLost {
			t.Fatalf("drops %v, removed %v, loot %+v; want the gel lost", dropsIn(s), removedDrops(s), l)
		}
	})
}

// A grant whose reply was lost is tried again under its key: one item, and the owner gets its stored reply.
func TestARetriedGrantGivesOneItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.lostReplies = 1
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		settle(time.Second + 2*worldtest.TickInterval)
		calls := items.made()
		if len(calls) != 2 || calls[0].Drops[0].Key != calls[1].Drops[0].Key || items.items() != 1 {
			t.Fatalf("%d calls, %d items; want a retry under the same key, one item", len(calls), items.items())
		}
		if ic := inventoryChanges(s); len(ic) != 1 || ic[0].Revision != 1 || len(dropsIn(s)) != 0 {
			t.Fatalf("inventory changes %+v, drops %v; want the stored reply", ic, dropsIn(s))
		}
	})
}

// An item service that never answers costs a call two timeouts; the drop waits, and flies once it answers.
func TestAHungItemServiceDelaysTheDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.hang = true
		inst, stop := runLootArena(t, data, items, 0.3)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		settle(11 * time.Second)
		if n := len(items.made()); n != 2 || len(dropsIn(s)) != 1 || len(lootOf(s)) != 0 {
			t.Fatalf("%d calls, drops %v; want two timed out, the drop still there, nothing told", n, dropsIn(s))
		}
		items.set(func(f *fakeItems) { f.hang = false })
		settle(3 * time.Second)
		if items.items() != 1 || len(dropsIn(s)) != 0 {
			t.Fatalf("%d items, drops %v; want the gel collected once the service answers", items.items(), dropsIn(s))
		}
	})
}

// An owner who left the map, through a portal or by logging out, still gets the drop: the grant needs no connection.
func TestAnOwnerWhoLeftStillGetsTheDrop(t *testing.T) {
	for _, leave := range []string{"portal", "logout"} {
		t.Run(leave, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				items := newFakeItems()
				items.gate = make(chan struct{})
				inst, stop := runLootArena(t, data, items, 0.3, worldtest.Options{Transfer: &transferRecorder{}})
				defer stop()
				settle(worldtest.TickInterval)
				p := warriorAt(250)
				p.Account = kernel.NewAccountID()
				s := join(t, inst, p)
				killSlime(t, inst, p.ID)
				settle(time.Second + worldtest.TickInterval)
				if len(items.made()) != 1 {
					t.Fatal("no call in flight")
				}
				if leave == "portal" {
					_ = inst.EnterPortal(p.ID)
				} else {
					_ = inst.Leave(p.ID, "", nil)
				}
				settle(worldtest.TickInterval)
				close(items.gate)
				settle(2 * worldtest.TickInterval)
				if items.items() != 1 || len(inventoryChanges(s)) != 1 || len(lootOf(s)) != 1 {
					t.Fatalf("%d items, %d changes, loot %+v; want the gel granted and told through the old sink", items.items(), len(inventoryChanges(s)), lootOf(s))
				}
			})
		})
	}
}

// A grant that finishes after its map stopped, with the map's inbox full, does not hold up the shutdown.
func TestAGrantAfterItsMapStoppedDoesNotHangTheWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.gate = make(chan struct{})
		workers := itemcalls.NewPool(itemcalls.Deps{Observer: idleJobs{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2})
		ctx, cancel := context.WithCancel(context.Background())
		workersDone := make(chan struct{})
		go func() { defer close(workersDone); _ = workers.Run(ctx) }()
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.3), Items: items, ItemCalls: workers})
		settle(worldtest.TickInterval)
		p := warriorAt(250)
		join(t, inst, p)
		killSlime(t, inst, p.ID)
		settle(time.Second + worldtest.TickInterval)
		if len(items.made()) != 1 {
			t.Fatal("no call in flight")
		}
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
			t.Fatal("the workers are stuck posting a grant's answer to a stopped map")
		}
	})
}

// idleJobs counts nothing; the loot tests read the item service's calls instead.
type idleJobs struct{}

func (idleJobs) CallQueued() {}
func (idleJobs) CallDone()   {}
