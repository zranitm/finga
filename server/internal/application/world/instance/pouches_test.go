package instance_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/itemcalls"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
	"finga/pkg/transient"
)

var errDown = errors.New("item module unavailable")

// fakeItems is the item module, idempotent by each item's key per account, as the real one is.
type fakeItems struct {
	mu          sync.Mutex
	calls       []model.GrantCall
	granted     map[grantKey]model.Inventory // each key's stored reply
	revision    uint64
	full        bool          // every item not granted before is refused for room
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

// items is how many items were granted, each once.
func (f *fakeItems) items() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.granted)
}

// pouchesIn are the player's own pouches on the ground as s saw them.
func pouchesIn(s *worldtest.Sink) map[uint64]model.PouchView {
	seen := map[uint64]model.PouchView{}
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			clear(seen) // another join's, whose pouches it lists anew
			for _, p := range ev.Pouches {
				seen[p.ID] = p
			}
		case worldtest.Delta:
			for _, p := range ev.Pouches.Changed {
				seen[p.ID] = p
			}
			for _, id := range ev.Pouches.Gone {
				delete(seen, id)
			}
		default: // the rest change no pouch
		}
	}
	return seen
}

// onlyPouch is the player's one pouch on the ground as s saw it.
func onlyPouch(t *testing.T, s *worldtest.Sink) model.PouchView {
	t.Helper()
	ps := pouchesIn(s)
	if len(ps) != 1 {
		t.Fatalf("pouches %+v, want one", ps)
	}
	var out model.PouchView
	for _, p := range ps {
		out = p
	}
	return out
}

// anyPouch reports whether s ever heard of a pouch.
func anyPouch(s *worldtest.Sink) bool {
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			if len(ev.Pouches) > 0 {
				return true
			}
		case worldtest.Delta:
			if len(ev.Pouches.Changed) > 0 || len(ev.Pouches.Gone) > 0 {
				return true
			}
		}
	}
	return false
}

func everything(p model.PouchView) []model.Pick {
	var out []model.Pick
	for _, it := range p.Items {
		out = append(out, model.Pick{Pouch: p.ID, Item: it.ID})
	}
	return out
}

func contents(p model.PouchView) map[string]int {
	out := map[string]int{}
	for _, it := range p.Items {
		out[it.Template] += it.Quantity
	}
	return out
}

// taken is what a take came to.
type taken struct {
	answers []model.PickAnswer
	err     error
}

// take asks for picks as the player, on a goroutine as a node's call is; answer reads it once it came.
func take(inst *instance.Instance, id kernel.CharacterID, picks ...model.Pick) <-chan taken {
	out := make(chan taken, 1)
	go func() {
		a, err := inst.TakeLoot(context.Background(), id, "", picks)
		out <- taken{a, err}
	}()
	return out
}

// answer is the take's answer, which must have come within a few ticks: one starts its call, the next settles it.
func answer(t *testing.T, c <-chan taken) taken {
	t.Helper()
	for range 5 {
		settle(worldtest.TickInterval)
		select {
		case a := <-c:
			return a
		default:
		}
	}
	t.Fatal("the take wasn't answered in five ticks")
	return taken{}
}

func inventoryChanges(s *worldtest.Sink) []model.InventoryChanged {
	return worldtest.Of[model.InventoryChanged](s)
}

func lootOf(s *worldtest.Sink) []model.Loot { return worldtest.Of[model.Loot](s) }

func itemsOf(l model.Loot) map[string]int {
	out := map[string]int{}
	for _, it := range l.Items {
		out[it.Template] += it.Quantity
	}
	return out
}

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
	m := arena(t, time.Hour)
	m.Portals = []gamedata.Portal{{Name: "to_town", At: gamedata.Point{X: arenaX, Z: nextToSlime - 0.8}, Radius: 1, TargetMap: "willowmere", TargetPortal: "north_pass"}} // round the killer, clear of the slime's reach
	inst, stop := worldtest.RunMap(t, data, m, nil, opts)
	return inst, func() { stop(); cancel(); <-workersDone }
}

// killSlime attacks until the arena slime is dead (two average hits), the second as the warrior's 900 ms attack ends.
func killSlime(t *testing.T, inst *instance.Instance, id kernel.CharacterID) {
	t.Helper()
	_ = inst.Attack(id, 0, model.EntityRef{})
	time.Sleep(18 * worldtest.TickInterval)
	synctest.Wait()
	_ = inst.Attack(id, 0, model.EntityRef{})
	time.Sleep(worldtest.TickInterval)
	synctest.Wait()
}

// lastMob is where s last saw a mob, removed or not.
func lastMob(s *worldtest.Sink) model.MobView {
	var last model.MobView
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			for _, mb := range ev.Mobs {
				last = mb
			}
		case worldtest.Delta:
			for _, mb := range ev.Mobs {
				last = mb
			}
		}
	}
	return last
}

// settle waits out d and lets the workers' answers land.
func settle(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// A kill leaves its killer one pouch where the slime died, holding its whole roll when the pouch's chance passes, else only what an active quest of theirs collects; nobody else hears of it.
func TestAKillLeavesItsKillerAPouch(t *testing.T) {
	data := worldtest.LoadData(t)
	collecting := model.Quests{"sticky_samples": {Status: model.QuestActive, Progress: []int{0}}}
	tests := []struct {
		name   string
		roll   float64
		quests model.Quests
		want   map[string]int // template → quantity; nil for no pouch
	}{ // the slime drops slime_gel 90% 1..2, red_potion 22.5% 1; a pouch falls 25% of the time
		{"a roll under the pouch's chance and both items' leaves both", 0.1, nil, map[string]int{"slime_gel": 1, "red_potion": 1}},
		{"a roll over the pouch's chance leaves nothing", 0.3, nil, nil},
		{"a roll over the pouch's chance still leaves what a quest collects", 0.3, collecting, map[string]int{"slime_gel": 1}},
		{"a roll over every chance leaves nothing, a quest or not", 0.95, collecting, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := runLootArena(t, data, newFakeItems(), tt.roll)
				defer stop()
				settle(worldtest.TickInterval)
				killer := warriorAt(nextToSlime)
				killer.Quests = tt.quests
				s, other := join(t, inst, killer), join(t, inst, warriorAt(spawnZ))
				killSlime(t, inst, killer.ID)
				if anyPouch(other) {
					t.Fatal("another player heard of the killer's pouch")
				}
				if tt.want == nil {
					if ps := pouchesIn(s); len(ps) != 0 {
						t.Fatalf("pouches %+v, want none", ps)
					}
					return
				}
				p := onlyPouch(t, s)
				if got := contents(p); !maps.Equal(got, tt.want) {
					t.Fatalf("the pouch holds %v, want %v", got, tt.want)
				}
				if mb := lastMob(s); p.Kind != model.PouchSmall || p.Y != 0 || math.Hypot(p.X-mb.X, p.Z-mb.Z) > 0.25 || p.GoneIn != data.Loot().PouchLife { // its last step, in the tick it died, goes unseen
					t.Fatalf("pouch %+v; want a pouch on the ground where the slime was last seen, (%v, %v), for its whole life", p, mb.X, mb.Z)
				}
			})
		})
	}
}

// A take grants the picked items to their owner as one call: they hear their inventory and the line, the emptied pouch goes, and nobody else hears a thing.
func TestATakeGrantsThePouchToItsOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		inst, stop := runLootArena(t, data, items, 0.1) // a gel and a potion
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		p.Account = kernel.NewAccountID()
		s, other := join(t, inst, p), join(t, inst, warriorAt(spawnZ))
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		settle(3 * time.Second)
		if len(items.made()) != 0 {
			t.Fatal("an item was granted without a take")
		}
		got := answer(t, take(inst, p.ID, everything(pouch)...))
		if got.err != nil || len(got.answers) != 2 || got.answers[0].Err != nil || got.answers[1].Err != nil {
			t.Fatalf("answered %+v; want both picks taken", got)
		}
		calls := items.made()
		if len(calls) != 1 || calls[0].Account != p.Account || calls[0].Character != p.ID || len(calls[0].Drops) != 2 {
			t.Fatalf("calls %+v; want the two in one call to the owner", calls)
		}
		settle(worldtest.TickInterval)
		if ps := pouchesIn(s); len(ps) != 0 {
			t.Fatalf("pouches %+v; want the emptied pouch gone", ps)
		}
		if ic := inventoryChanges(s); len(ic) != 1 || ic[0].Revision != 1 || len(ic[0].Slots) != 2 {
			t.Fatalf("inventory changes %+v", ic)
		}
		l := lootOf(s)
		if len(l) != 1 || l[0].Kind != model.LootTaken || l[0].Pouch != pouch.ID || l[0].Owner != p.ID || l[0].OwnerName != "Ayla" || l[0].MapID != "arena" ||
			!maps.Equal(itemsOf(l[0]), map[string]int{"slime_gel": 1, "red_potion": 1}) {
			t.Fatalf("loot %+v", l)
		}
		if anyPouch(other) || len(lootOf(other)) != 0 || len(inventoryChanges(other)) != 0 {
			t.Fatal("a player outside the party heard of the killer's pouch or loot")
		}
	})
}

// A player joining sees only their own pouches: a stranger's Snapshot has none, and the owner's, joining again, has theirs.
func TestAJoinSeesOnlyTheJoinersPouches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := runLootArena(t, data, newFakeItems(), 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		if late := join(t, inst, warriorAt(spawnZ)); anyPouch(late) {
			t.Fatal("a stranger's Snapshot holds the killer's pouch")
		}
		_ = inst.Leave(p.ID, "", nil)
		settle(worldtest.TickInterval)
		again := join(t, inst, p)
		if got := pouchesIn(again); len(got) != 1 || got[pouch.ID].ID != pouch.ID || len(got[pouch.ID].Items) != 2 {
			t.Fatalf("the owner joined again to %+v, want their pouch", got)
		}
	})
}

// A party's pouch goes to its next member alive on the map, and each member there hears what it took.
func TestAPartysPouchGoesToItsNextMemberOnTheMap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		killer, mate, dead := warriorAt(nextToSlime), warriorAt(16), warriorAt(nextToSlime)
		dead.HP = 1 // comes alone into the slime's reach and dies to its hop, so it goes after no one else first
		away := kernel.NewCharacterID()
		parties := &fakeParties{parties: [][]kernel.CharacterID{{dead.ID, away, killer.ID, mate.ID}}}
		inst, stop := runLootArena(t, data, items, 0.1, worldtest.Options{Parties: parties})
		defer stop()
		settle(worldtest.TickInterval)
		untilDead(t, join(t, inst, dead), dead.ID)
		sk, sm := join(t, inst, killer), join(t, inst, mate)
		stranger := join(t, inst, warriorAt(15))
		killSlime(t, inst, killer.ID)
		if anyPouch(sm) || len(pouchesIn(sk)) != 1 {
			t.Fatal("want the pouch the killer's, the dead and the absent skipped, the mate seeing none")
		}
		if got := answer(t, take(inst, killer.ID, everything(onlyPouch(t, sk))...)); got.err != nil {
			t.Fatal(got.err)
		}
		settle(worldtest.TickInterval)
		for _, s := range []*worldtest.Sink{sk, sm} {
			if l := lootOf(s); len(l) != 1 || l[0].Kind != model.LootTaken || l[0].Owner != killer.ID || len(l[0].Items) != 2 {
				t.Fatalf("loot %+v; want both members to hear the killer took both", l)
			}
		}
		if len(inventoryChanges(sk)) != 1 || len(inventoryChanges(sm)) != 0 || len(lootOf(stranger)) != 0 {
			t.Fatal("an inventory change went to the wrong player, or a stranger heard the party's loot")
		}
	})
}

// The world refuses a take of a dead player, and one of a pouch beyond reach of where the server has the player.
func TestATakeIsRefusedDeadOrOutOfReach(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		inst, stop := runLootArena(t, data, items, 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		doomed := warriorAt(nextToSlime)
		doomed.HP = 1
		untilDead(t, join(t, inst, doomed), doomed.ID)
		if got := answer(t, take(inst, doomed.ID, model.Pick{Pouch: 1, Item: 1})); !errors.Is(got.err, model.ErrCharacterDead) {
			t.Fatalf("a dead player's take answered %+v, want dead", got)
		}
		settle(5 * time.Second) // the slime goes home
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		for seq := range uint32(20) {
			_ = inst.Input(p.ID, seq+1, sim.Input{Down: true}, 0)
			settle(worldtest.TickInterval)
		}
		got := answer(t, take(inst, p.ID, everything(pouch)...))
		if got.err != nil || !errors.Is(got.answers[0].Err, model.ErrPouchOutOfReach) || len(items.made()) != 0 {
			t.Fatalf("a take from a second's walk away answered %+v; want out of reach, nothing granted", got)
		}
	})
}

// A full tab leaves the item in its pouch, answered with the tab; once there's room it's taken.
func TestAFullTabLeavesTheItemInItsPouch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.full = true
		inst, stop := runLootArena(t, data, items, 0.3, worldtest.Options{}) // a gel, for the quest
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		p.Quests = model.Quests{"sticky_samples": {Status: model.QuestActive, Progress: []int{0}}}
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		got := answer(t, take(inst, p.ID, everything(pouch)...))
		if got.err != nil || !errors.Is(got.answers[0].Err, kernel.ErrEtcTabFull) {
			t.Fatalf("answered %+v, want the gel refused for the Etc tab", got)
		}
		settle(worldtest.TickInterval)
		if ps := pouchesIn(s); len(ps) != 1 || len(lootOf(s)) != 0 || items.items() != 0 {
			t.Fatalf("pouches %+v, loot %+v; want the gel left in its pouch", ps, lootOf(s))
		}
		items.set(func(f *fakeItems) { f.full = false })
		if got := answer(t, take(inst, p.ID, everything(pouch)...)); got.err != nil || got.answers[0].Err != nil || items.items() != 1 {
			t.Fatalf("answered %+v; want the gel taken once there was room", got)
		}
	})
}

// A take whose reply was lost is tried again under its key: one item, and the owner gets its stored reply.
func TestARetriedGrantGivesOneItem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.lostReplies = 1
		inst, stop := runLootArena(t, data, items, 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		got := answer(t, take(inst, p.ID, everything(onlyPouch(t, s))...))
		calls := items.made()
		if got.err != nil || got.answers[0].Err != nil || len(calls) != 2 || calls[0].Drops[0].Key != calls[1].Drops[0].Key || items.items() != 2 {
			t.Fatalf("answered %+v after %d calls, %d items; want a retry under the same keys, each item once", got, len(calls), items.items())
		}
		settle(worldtest.TickInterval)
		if ic := inventoryChanges(s); len(ic) != 2 || ic[0].Revision != 1 || len(pouchesIn(s)) != 0 {
			t.Fatalf("inventory changes %+v; want each item's stored reply", ic)
		}
	})
}

// An item module that never answers costs a take two timeouts: its items stay taken, asked again under their keys, and are taken once it answers.
func TestAHungItemServiceDelaysTheTake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.hang = true
		inst, stop := runLootArena(t, data, items, 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		asked := take(inst, p.ID, everything(pouch)...)
		settle(10 * time.Second)
		got := answer(t, asked)
		if n := len(items.made()); n != 2 || got.err != nil || got.answers[0].Err == nil || len(pouchesIn(s)) != 1 || len(lootOf(s)) != 0 {
			t.Fatalf("%d calls, answered %+v; want two timed out, the pick answered to try again, the pouch still there", n, got)
		}
		items.set(func(f *fakeItems) { f.hang = false })
		settle(13 * time.Second) // the second try's two timeouts end, then the next goes
		if items.items() != 2 || len(pouchesIn(s)) != 0 || len(lootOf(s)) != 1 {
			t.Fatalf("%d items, pouches %v; want both taken once the module answers", items.items(), pouchesIn(s))
		}
	})
}

// A take whose grant starts late, the queues and ticks before it having taken most of the server's 5 s, answers each pick unavailable half a second before the command's deadline, never with the deadline, which the server would answer as a fault for the whole take; its items stay taken, and come once the module answers.
func TestALateTakeAnswersEachPickUnavailableBeforeItsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		items := newFakeItems()
		items.hang = true
		inst, stop := runLootArena(t, data, items, 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		began := time.Now()
		got, err := inst.TakeLoot(ctx, p.ID, "", everything(pouch))
		if took := time.Since(began); err != nil || took != 1500*time.Millisecond || len(got) != len(everything(pouch)) || slices.ContainsFunc(got, func(a model.PickAnswer) bool { return !errors.Is(a.Err, transient.ErrUnavailable) }) {
			t.Fatalf("%+v, %v after %v; want each pick unavailable at 1.5 s, half a second before the command's 2", got, err, took)
		}
		items.set(func(f *fakeItems) { f.hang = false })
		settle(13 * time.Second) // the grant's two timeouts end, then the next try goes
		if len(pouchesIn(s)) != 0 || len(lootOf(s)) != 1 {
			t.Fatalf("pouches %v, loot %v; want the items taken once the module answers", pouchesIn(s), lootOf(s))
		}
	})
}

// An owner who left the map, through a portal or by logging out, still gets the items their take was granting: the grant needs no connection.
func TestAnOwnerWhoLeftStillGetsWhatTheyTook(t *testing.T) {
	for _, leave := range []string{"portal", "logout"} {
		t.Run(leave, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				items := newFakeItems()
				items.gate = make(chan struct{})
				inst, stop := runLootArena(t, data, items, 0.1, worldtest.Options{Transfer: &transferRecorder{}})
				defer stop()
				settle(worldtest.TickInterval)
				p := warriorAt(nextToSlime)
				p.Account = kernel.NewAccountID()
				s := join(t, inst, p)
				killSlime(t, inst, p.ID)
				take(inst, p.ID, everything(onlyPouch(t, s))...)
				settle(worldtest.TickInterval)
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
				if items.items() != 2 || len(inventoryChanges(s)) != 1 || len(lootOf(s)) != 1 {
					t.Fatalf("%d items, %d changes, loot %+v; want both granted and told through the old sink", items.items(), len(inventoryChanges(s)), lootOf(s))
				}
			})
		})
	}
}

// A pouch nobody takes is gone at the end of its minute, and its owner hears what was left.
func TestAPouchLeftAMinuteIsGone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := runLootArena(t, data, newFakeItems(), 0.1)
		defer stop()
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		pouch := onlyPouch(t, s)
		settle(58 * time.Second)
		if len(pouchesIn(s)) != 1 {
			t.Fatal("the pouch went early")
		}
		settle(3 * time.Second)
		l := lootOf(s)
		if len(pouchesIn(s)) != 0 || len(l) != 1 || l[0].Kind != model.LootLeft || l[0].Pouch != pouch.ID || !maps.Equal(itemsOf(l[0]), contents(pouch)) {
			t.Fatalf("pouches %v, loot %+v; want the pouch gone and its two said left", pouchesIn(s), l)
		}
	})
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
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{NewRoll: worldtest.Rolls(0.1), Items: items, ItemCalls: workers})
		settle(worldtest.TickInterval)
		p := warriorAt(nextToSlime)
		s := join(t, inst, p)
		killSlime(t, inst, p.ID)
		asked := take(inst, p.ID, everything(onlyPouch(t, s))...)
		settle(worldtest.TickInterval)
		if len(items.made()) != 1 {
			t.Fatal("no call in flight")
		}
		stop()
		if got := answer(t, asked); !errors.Is(got.err, model.ErrStopped) {
			t.Fatalf("the take in flight answered %+v once its map stopped, want stopped", got)
		}
		for n := uint32(100); inst.Input(p.ID, n, sim.Input{}, 0) == nil; n++ {
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

// idleJobs counts nothing; the loot tests read the item module's calls instead.
type idleJobs struct{}

func (idleJobs) CallQueued() {}
func (idleJobs) CallDone()   {}
