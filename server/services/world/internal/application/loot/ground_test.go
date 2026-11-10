package loot_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

const tick = 50 * time.Millisecond

// A slime that always drops two gels; a trio that always drops three things.
var (
	slime = gamedata.Mob{ID: "green_slime", Drops: []gamedata.Drop{{Item: "slime_gel", Chance: 1, Min: 2, Max: 2}}}
	trio  = gamedata.Mob{ID: "trio", Drops: []gamedata.Drop{
		{Item: "slime_gel", Chance: 1, Min: 1, Max: 1}, {Item: "red_potion", Chance: 1, Min: 1, Max: 1}, {Item: "mushroom_cap", Chance: 1, Min: 1, Max: 1},
	}}
	// A boar that always drops an iron sword, whose attack rolls in 16 to 20 and nothing else.
	boar = gamedata.Mob{ID: "boar", Drops: []gamedata.Drop{{Item: "iron_sword", Chance: 1, Min: 1, Max: 1}}}
)

var errDown = errors.New("item service down")

// answer is how the item service answers one call.
type answer func(n int, c model.GrantCall) (model.Granted, error)

// every answers each drop of a call the same way; added ones each fill a slot of their own, at the call's number as the revision.
func every(kind model.GrantKind) answer {
	return func(n int, c model.GrantCall) (model.Granted, error) {
		kinds := make([]model.GrantKind, len(c.Drops))
		for k := range kinds {
			kinds[k] = kind
		}
		return outcomes(kinds...)(n, c)
	}
}

// outcomes answers each drop of a call in turn.
func outcomes(kinds ...model.GrantKind) answer {
	return func(n int, c model.GrantCall) (model.Granted, error) {
		out := model.Granted{Added: model.Inventory{Revision: uint64(n)}}
		for k, kind := range kinds {
			o := model.GrantOutcome{Kind: kind}
			if kind == model.GrantRefused {
				o.Reason = "REASON_BAD_ROLL"
			}
			out.Outcomes = append(out.Outcomes, o)
			if kind == model.GrantAdded {
				out.Added.Slots = append(out.Added.Slots, model.Slot{Tab: gamedata.Etc, Slot: k, Item: &model.SlotItem{Template: c.Drops[k].Template, Quantity: c.Drops[k].Quantity}})
			}
		}
		return out, nil
	}
}

func down(int, model.GrantCall) (model.Granted, error) { return model.Granted{}, errDown }

// grants is the item service: each call takes the next answer, the last one again once they run out.
type grants struct {
	mu      sync.Mutex
	answers []answer
	calls   []model.GrantCall
	spans   []trace.SpanContext // each call's
	gate    chan struct{}       // when set, each call waits for it to close
}

func (f *grants) AddMany(ctx context.Context, c model.GrantCall) (model.Granted, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.spans = append(f.spans, trace.SpanContextFromContext(ctx))
	n := len(f.calls)
	a, gate := f.answers[min(n, len(f.answers))-1], f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return a(n, c)
}

func (f *grants) Retryable(err error) bool { return errors.Is(err, errDown) }

func (f *grants) made() []model.GrantCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *grants) spanOf(k int) trace.SpanContext {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spans[k]
}

func (f *grants) set(change func(f *grants)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

// batches records each call's size.
type batches struct {
	mu    sync.Mutex
	sizes []int
}

func (b *batches) ObserveBatch(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sizes = append(b.sizes, n)
}

// syncWriter is a log the workers write and the test reads.
type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type jobs struct{}

func (jobs) CallQueued() {}
func (jobs) CallDone()   {}

// options shape a scene: the map's random source, the drops per call, and whether the workers start.
type options struct {
	roll     model.Roll
	batchMax int
	idle     bool
}

// scene is a map's ground on its own: each step is a tick that settles the answers back, then collects, and lets the workers answer.
type scene struct {
	t       *testing.T
	g       *loot.Ground
	items   *grants
	workers *itemcalls.Pool
	run     func() // starts the workers of an idle scene
	results chan loot.Result
	batches *batches
	logs    *syncWriter
	spans   *tracetest.SpanRecorder
	tick    uint64
	// The loot timings in ticks.
	delay, life, roomCheck uint64
}

func newScene(t *testing.T, items *grants) *scene {
	t.Helper()
	return sceneWith(t, items, options{})
}

func sceneWith(t *testing.T, items *grants, o options) *scene {
	t.Helper()
	if o.roll == nil {
		o.roll = worldtest.FixedRoll{V: 0}
	}
	if o.batchMax == 0 {
		o.batchMax = 32
	}
	data := worldtest.LoadData(t)
	ticks := func(d time.Duration) uint64 { return model.TicksIn(d, tick) }
	s := &scene{
		t: t, items: items, results: make(chan loot.Result, 64), batches: &batches{}, logs: &syncWriter{}, spans: tracetest.NewSpanRecorder(),
		workers: itemcalls.NewPool(itemcalls.Deps{Observer: jobs{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2}),
		delay:   ticks(data.Loot().CollectDelay), life: ticks(data.Loot().DropLife), roomCheck: ticks(data.Loot().RoomCheckEvery),
	}
	var once sync.Once
	s.run = func() {
		once.Do(func() {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); _ = s.workers.Run(ctx) }()
			t.Cleanup(func() { cancel(); <-done })
		})
	}
	if !o.idle {
		s.run()
	}
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.spans))
	s.g = loot.NewGround(loot.Deps{
		MapID: "forest", Items: items, ItemCalls: s.workers, Observer: s.batches, Traces: traces, Roll: o.roll, Data: data,
		Logger: slog.New(slog.NewTextHandler(s.logs, nil)), Settle: func(_ context.Context, r loot.Result) { s.results <- r },
	}, loot.Tuning{TickInterval: tick, BatchMax: o.batchMax})
	s.g.Begin(0)
	return s
}

// step runs one tick: the answers back settle, then due drops go; the workers answer before it returns.
func (s *scene) step() []loot.Settled {
	s.tick++
	s.g.Begin(s.tick)
	var out []loot.Settled
	for {
		select {
		case r := <-s.results:
			out = append(out, s.g.Settle(r))
			continue
		default:
		}
		break
	}
	out = append(out, s.g.Collect()...)
	synctest.Wait()
	return out
}

// until steps to tick n, returning all that settled.
func (s *scene) until(n uint64) []loot.Settled {
	var out []loot.Settled
	for s.tick < n {
		out = append(out, s.step()...)
	}
	return out
}

func owner(name string) loot.Owner {
	return loot.Owner{ID: kernel.NewCharacterID(), Account: kernel.NewAccountID(), Name: name}
}

// solo is a kill by o alone.
func solo(mob gamedata.Mob, o loot.Owner) loot.Kill {
	return loot.Kill{Mob: mob, X: 200, Y: 400, Killer: o, Eligible: []loot.Owner{o}}
}

// partyKill is a kill by killer in party, whose eligible members are those given.
func partyKill(mob gamedata.Mob, killer loot.Owner, party model.Party, eligible ...loot.Owner) loot.Kill {
	return loot.Kill{Mob: mob, X: 200, Y: 400, Killer: killer, Party: party, Eligible: eligible}
}

func partyOf(id string, members ...loot.Owner) model.Party {
	p := model.Party{ID: id}
	for _, m := range members {
		p.Members = append(p.Members, m.ID)
	}
	return p
}

// owners are the owners of the drops on the ground, oldest first.
func owners(g *loot.Ground) []kernel.CharacterID {
	var out []kernel.CharacterID
	for _, d := range g.Views() {
		out = append(out, d.Owner)
	}
	return out
}

// loot is every Loot in what settled, in order.
func lootIn(settled []loot.Settled) []model.Loot {
	var out []model.Loot
	for _, s := range settled {
		out = append(out, s.Loot...)
	}
	return out
}

func kinds(ls []model.Loot) []model.LootKind {
	var out []model.LootKind
	for _, l := range ls {
		out = append(out, l.Kind)
	}
	return out
}

// Equipment rolls its stats as it drops, from the map's random source, and its grant carries the roll; anything else has none.
func TestEquipmentRollsItsStatsAsItDrops(t *testing.T) {
	for _, tt := range []struct {
		name   string
		roll   float64
		attack int
	}{
		{"a roll of 0 takes the bottom of each range", 0, 16},
		{"a roll of a half, the middle", 0.5, 18},
		{"a roll just under 1, the top", 0.999, 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: worldtest.FixedRoll{V: tt.roll}})
				s.g.Roll(solo(boar, owner("Ayla")))
				s.until(s.delay)
				if got := s.items.made(); len(got) != 1 || got[0].Drops[0].Stats == nil || *got[0].Drops[0].Stats != (progression.ItemStats{PhysicalAttack: tt.attack}) {
					t.Fatalf("granted %+v, want a sword of attack %d", got, tt.attack)
				}
			})
		})
	}
	t.Run("a gel has no roll", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
			s.g.Roll(solo(slime, owner("Ayla")))
			s.until(s.delay)
			if got := s.items.made(); len(got) != 1 || got[0].Drops[0].Stats != nil {
				t.Fatalf("granted %+v, want no roll", got)
			}
		})
	})
}

// Rolls come from the map's seeded source, so a recorded fight replays the same loot.
func TestTheSameSeedRollsTheSameStats(t *testing.T) {
	const drops = 5
	rolled := func(t *testing.T) []progression.ItemStats {
		t.Helper()
		var out []progression.ItemStats
		synctest.Test(t, func(t *testing.T) {
			s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: rand.New(rand.NewPCG(1, 2))})
			ayla := owner("Ayla")
			for range drops {
				s.g.Roll(solo(boar, ayla))
			}
			s.until(s.delay)
			for _, d := range s.items.made()[0].Drops {
				out = append(out, *d.Stats)
			}
		})
		return out
	}
	first, second := rolled(t), rolled(t)
	if len(first) != drops || !slices.Equal(first, second) {
		t.Fatalf("rolled %v, then %v from the same seed", first, second)
	}
	sword, _ := worldtest.LoadData(t).Item("iron_sword")
	for _, s := range first {
		if !s.Fits(sword.Rolls) {
			t.Fatalf("rolled %+v, outside the sword's ranges", s)
		}
	}
}

func TestADropIsTheKillersWithoutAParty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
		ayla, bo := owner("Ayla"), owner("Bo")
		s.g.Roll(solo(trio, ayla))
		s.g.Roll(partyKill(slime, ayla, partyOf("p1", ayla, bo), ayla)) // her party, with nobody else eligible
		s.g.Roll(partyKill(slime, ayla, partyOf("p1", ayla, bo)))       // nobody eligible at all
		if got := owners(s.g); len(got) != 5 || slices.ContainsFunc(got, func(id kernel.CharacterID) bool { return id != ayla.ID }) {
			t.Fatalf("owners %v, want all five Ayla's", got)
		}
	})
}

func TestAPartyTakesItsDropsInTurn(t *testing.T) {
	ann, bo, cy, dee := owner("Ann"), owner("Bo"), owner("Cy"), owner("Dee")
	all := partyOf("p1", ann, bo, cy)
	tests := []struct {
		name  string
		kills func(s *scene)
		want  []loot.Owner
	}{
		{"one kill's drops go one to each member, from the first in join order", func(s *scene) {
			s.g.Roll(partyKill(trio, bo, all, ann, bo, cy))
		}, []loot.Owner{ann, bo, cy}},
		{"the turn carries over from kill to kill, and wraps", func(s *scene) {
			for _, killer := range []loot.Owner{cy, cy, ann, bo} {
				s.g.Roll(partyKill(slime, killer, all, ann, bo, cy))
			}
		}, []loot.Owner{ann, bo, cy, ann}},
		{"a member not eligible, dead or on another map, is skipped", func(s *scene) {
			s.g.Roll(partyKill(trio, ann, all, ann, cy))
		}, []loot.Owner{ann, cy, ann}},
		{"a member who leaves doesn't reset the turn: it goes on from the order before", func(s *scene) {
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			left := partyOf("p1", ann, cy)
			s.g.Roll(partyKill(slime, ann, left, ann, cy))
			s.g.Roll(partyKill(slime, ann, left, ann, cy))
		}, []loot.Owner{ann, bo, cy, ann}},
		{"when everyone after the last recipient left, it goes round to the first", func(s *scene) {
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann), ann))
		}, []loot.Owner{ann, bo, ann}},
		{"when everyone in the round left, it starts from the first member", func(s *scene) {
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann, bo))
			s.g.Roll(partyKill(slime, cy, partyOf("p1", cy, dee), cy, dee))
		}, []loot.Owner{ann, cy}},
		{"a member who joins comes after the others", func(s *scene) {
			s.g.Roll(partyKill(trio, ann, all, ann, bo, cy))
			grown := partyOf("p1", ann, bo, cy, dee)
			s.g.Roll(partyKill(slime, ann, grown, ann, bo, cy, dee))
		}, []loot.Owner{ann, bo, cy, dee}},
		{"each party keeps its own turn", func(s *scene) {
			other := partyOf("p2", dee, cy)
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann, bo))
			s.g.Roll(partyKill(slime, dee, other, dee, cy))
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann, bo))
		}, []loot.Owner{ann, dee, bo}},
		{"a turn unused for ten minutes starts again from the first member", func(s *scene) {
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			s.tick += uint64(10*time.Minute/tick) + 1
			s.g.Begin(s.tick)
			s.g.Collect()
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
		}, []loot.Owner{ann, ann}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{idle: true})
				tt.kills(s)
				var want []kernel.CharacterID
				for _, o := range tt.want {
					want = append(want, o.ID)
				}
				if got := owners(s.g); !slices.Equal(got, want) {
					t.Fatalf("owners %v, want %v", got, want)
				}
			})
		})
	}
}

func TestADropFliesToItsOwnerAfterItsDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		gel := s.g.Views()[0]
		if gel.Owner != ayla.ID {
			t.Fatalf("the drop shows owner %v, want Ayla", gel.Owner)
		}
		if s.until(s.delay - 1); len(s.items.made()) != 0 {
			t.Fatal("a drop went before its delay")
		}
		s.step()
		calls := s.items.made()
		if len(calls) != 1 || calls[0].Account != ayla.Account || calls[0].Character != ayla.ID || len(calls[0].Drops) != 1 || calls[0].Drops[0].Template != "slime_gel" || calls[0].Drops[0].Quantity != 2 {
			t.Fatalf("calls %+v, want Ayla's gel at its delay, alone", calls)
		}
		if len(s.g.Views()) != 1 {
			t.Fatal("the drop left before its grant was answered")
		}
		settled := s.step()
		if len(settled) != 1 || settled[0].Owner != ayla {
			t.Fatalf("settled %+v, want Ayla's", settled)
		}
		want := model.Loot{Tick: s.delay + 1, Kind: model.LootCollected, MapID: "forest", DropID: gel.ID, Owner: ayla.ID, OwnerName: "Ayla", Template: "slime_gel", Quantity: 2}
		if l := settled[0].Loot; len(l) != 1 || l[0] != want {
			t.Fatalf("loot %+v, want %+v", l, want)
		}
		if inv := settled[0].Inventory; len(inv) != 1 || inv[0].Revision != 1 || len(inv[0].Slots) != 1 || inv[0].Slots[0].Item.Template != "slime_gel" || inv[0].Tick != s.delay+1 {
			t.Fatalf("inventory changes %+v, want the gel's slot at the call's revision", inv)
		}
		added, removed, collected := s.g.Changes()
		if len(s.g.Views()) != 0 || len(added) != 0 || len(removed) != 0 || !slices.Equal(collected, []uint64{gel.ID}) {
			t.Fatalf("views %v, removed %v, collected %v; want the gel collected", s.g.Views(), removed, collected)
		}
		if s.batches.sizes[0] != 1 {
			t.Fatalf("batch sizes %v", s.batches.sizes)
		}
	})
}

// With the pool's queue full a due drop waits, and goes on a later tick; nobody is told.
func TestADueDropWaitsForTheQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{idle: true})
		for s.workers.Submit(func(context.Context) {}) {
		}
		s.g.Roll(solo(slime, owner("Ayla")))
		if settled := s.until(s.delay + 3); len(lootIn(settled)) != 0 || len(s.batches.sizes) != 0 {
			t.Fatalf("settled %+v, batches %v; want nothing while the queue is full", settled, s.batches.sizes)
		}
		s.run()
		synctest.Wait()
		s.step()
		if got := kinds(lootIn(s.step())); !slices.Equal(got, []model.LootKind{model.LootCollected}) {
			t.Fatalf("loot %v, want the gel collected once the queue had room", got)
		}
	})
}

// A full inventory keeps the drops on the ground, owned: their owner hears once, and one call every room check tries them all.
func TestAFullInventoryKeepsTheDropsWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantNoRoom)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		settled := s.until(s.delay + 1)
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootWaiting, model.LootWaiting, model.LootWaiting}) || len(s.g.Views()) != 3 {
			t.Fatalf("loot %v with %d drops left; want all three waiting, on the ground", got, len(s.g.Views()))
		}
		var at []uint64 // the ticks a call went
		for range 5 * s.roomCheck {
			n := len(items.made())
			if lootIn(s.step()) != nil {
				t.Fatal("a waiting drop was told again")
			}
			if len(items.made()) > n {
				at = append(at, s.tick)
			}
		}
		for k := 1; k < len(at); k++ {
			if gap := at[k] - at[k-1]; gap < s.roomCheck || gap > s.roomCheck+1 {
				t.Fatalf("calls at ticks %v; want one per room check", at)
			}
		}
		for _, c := range items.made() {
			if len(c.Drops) != 3 {
				t.Fatalf("a call carried %d drops, want the three waiting together", len(c.Drops))
			}
		}
		items.set(func(f *grants) { f.answers = []answer{every(model.GrantAdded)} })
		settled = s.until(s.tick + s.roomCheck + 2)
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootCollected, model.LootCollected, model.LootCollected}) || len(s.g.Views()) != 0 {
			t.Fatalf("loot %v, want the three collected within a room check of room", got)
		}
	})
}

// A drop still waiting for room at the end of its life is lost, and tried no more.
func TestADropWaitingAtTheEndOfItsLifeIsLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantNoRoom)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		gel := s.g.Views()[0]
		s.until(s.life - 1)
		if len(s.g.Views()) != 1 {
			t.Fatal("the drop left before its life ended")
		}
		var settled []loot.Settled
		for lootIn(settled) == nil && s.tick <= s.life+1 {
			settled = s.step() // lost at the end of its life, or at the answer of a call then in flight
		}
		want := model.Loot{Tick: s.tick, Kind: model.LootLost, MapID: "forest", DropID: gel.ID, Owner: ayla.ID, OwnerName: "Ayla", Template: "slime_gel", Quantity: 2}
		if l := lootIn(settled); len(l) != 1 || l[0] != want || settled[0].Owner != ayla {
			t.Fatalf("settled %+v, want the gel lost, told to Ayla", settled)
		}
		if _, removed, collected := s.g.Changes(); !slices.Equal(removed, []uint64{gel.ID}) || len(collected) != 0 {
			t.Fatalf("removed %v, collected %v", removed, collected)
		}
		calls := len(items.made())
		s.until(s.tick + 3*s.roomCheck)
		if len(items.made()) != calls {
			t.Fatal("a lost drop was tried again")
		}
	})
}

// A call the item service never answers may have committed: its drops wait, and go again under the same keys.
func TestAnUnansweredCallIsTriedAgainUnderTheSameKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{down, down, every(model.GrantAdded)}}
		s := newScene(t, items)
		s.g.Roll(solo(slime, owner("Ayla")))
		if settled := s.until(s.delay + 1); lootIn(settled) != nil || len(items.made()) != 2 || len(s.g.Views()) != 1 {
			t.Fatalf("loot %v after %d calls; want a quick retry on the worker, then nothing told", lootIn(settled), len(items.made()))
		}
		ended := s.spans.Ended()
		if len(ended) != 1 || ended[0].Status().Code != codes.Error || !strings.Contains(s.logs.String(), "a grant call failed") {
			t.Fatalf("%d spans, logs %q; want the failed call's span and a warning", len(ended), s.logs.String())
		}
		if s.until(s.tick + s.roomCheck - 1); len(items.made()) != 2 {
			t.Fatal("tried again before the room check")
		}
		settled := s.until(s.tick + 2)
		calls := items.made()
		if got := kinds(lootIn(settled)); len(calls) != 3 || calls[2].Drops[0].Key != calls[0].Drops[0].Key || !slices.Equal(got, []model.LootKind{model.LootCollected}) {
			t.Fatalf("calls %+v, loot %v; want the gel granted under its first key", calls, got)
		}
	})
}

// A drop whose call is answered after its life ended: no room loses it; no answer gives it one last try under its key.
func TestAnAnswerAfterTheDropsLifeEnded(t *testing.T) {
	tests := []struct {
		name    string
		answers []answer
		calls   int
		want    model.LootKind
	}{
		{"no room: lost", []answer{every(model.GrantNoRoom)}, 1, model.LootLost},
		{"no answer, then the last try grants it", []answer{down, down, every(model.GrantAdded)}, 3, model.LootCollected},
		{"no answer, nor to the last try: lost", []answer{down}, 4, model.LootLost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				items := &grants{answers: tt.answers, gate: make(chan struct{})}
				s := newScene(t, items)
				s.g.Roll(solo(slime, owner("Ayla")))
				s.until(s.life + 1)
				if len(s.g.Views()) != 1 {
					t.Fatal("a drop left with its call in flight")
				}
				close(items.gate)
				synctest.Wait()
				settled := s.until(s.tick + 3)
				if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{tt.want}) || len(items.made()) != tt.calls || len(s.g.Views()) != 0 {
					t.Fatalf("loot %v after %d calls, want %v after %d", got, len(items.made()), tt.want, tt.calls)
				}
			})
		})
	}
}

// A drop never sent, the pool full until its life ended, still has its one try once a worker is free.
func TestADropNeverSentHasATryPastItsLife(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{idle: true})
		for s.workers.Submit(func(context.Context) {}) {
		}
		s.g.Roll(solo(slime, owner("Ayla")))
		s.until(s.life + 2)
		if len(s.g.Views()) != 1 {
			t.Fatal("the drop left with no worker free for its try")
		}
		s.run()
		synctest.Wait()
		if got := kinds(lootIn(s.until(s.tick + 2))); !slices.Equal(got, []model.LootKind{model.LootCollected}) {
			t.Fatalf("loot %v, want the gel collected", got)
		}
	})
}

// A drop refused by another rule never passes on a retry: it is lost at once, and the log says why.
func TestADropRefusedByAnotherRuleIsLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{every(model.GrantRefused)}})
		s.g.Roll(solo(slime, owner("Ayla")))
		settled := s.until(s.delay + 1)
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootLost}) || len(s.g.Views()) != 0 {
			t.Fatalf("loot %v, want the gel lost", got)
		}
		if logs := s.logs.String(); !strings.Contains(logs, "refused by a rule") || !strings.Contains(logs, "REASON_BAD_ROLL") {
			t.Fatalf("logs %q; want the refusal and its reason", logs)
		}
	})
}

// Each drop of a call settles from its own answer, and the owner gets the slots the call's grants changed once.
func TestEachDropOfACallSettlesOnItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{outcomes(model.GrantAdded, model.GrantNoRoom, model.GrantRefused)}})
		s.g.Roll(solo(trio, owner("Ayla")))
		drops := s.g.Views()
		settled := s.until(s.delay + 1)
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootCollected, model.LootWaiting, model.LootLost}) {
			t.Fatalf("loot %v", got)
		}
		if inv := settled[0].Inventory; len(inv) != 1 || len(inv[0].Slots) != 1 || inv[0].Slots[0].Item.Template != "slime_gel" {
			t.Fatalf("inventory changes %+v, want the one the gel's grant made", inv)
		}
		if _, removed, collected := s.g.Changes(); !slices.Equal(collected, []uint64{drops[0].ID}) || !slices.Equal(removed, []uint64{drops[2].ID}) || len(s.g.Views()) != 1 || s.g.Views()[0].ID != drops[1].ID {
			t.Fatalf("collected %v, removed %v, left %v", collected, removed, s.g.Views())
		}
	})
}

// A drop granted by an earlier call answers that call's stored reply, sent before this call's own; one granted under another fingerprint has none to send.
func TestADropGrantedBeforeAnswersItsStoredReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		potion := model.Slot{Tab: gamedata.Use, Slot: 3, Item: &model.SlotItem{Template: "red_potion", Quantity: 1}}
		replayed := func(n int, c model.GrantCall) (model.Granted, error) {
			out, _ := outcomes(model.GrantAdded, model.GrantReplayed, model.GrantReplayed)(n, c)
			out.Outcomes[1].Replayed = model.Inventory{Revision: 4, Slots: []model.Slot{potion}}
			out.Added.Revision = 9
			return out, nil
		}
		s := newScene(t, &grants{answers: []answer{replayed}})
		s.g.Roll(solo(trio, owner("Ayla")))
		settled := s.until(s.delay + 1)
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootCollected, model.LootCollected, model.LootCollected}) {
			t.Fatalf("loot %v, want all three collected", got)
		}
		inv := settled[0].Inventory
		if len(inv) != 2 || inv[0].Revision != 4 || inv[0].Slots[0] != potion || inv[1].Revision != 9 {
			t.Fatalf("inventory changes %+v; want the stored reply at 4, then the call's at 9", inv)
		}
	})
}

// An owner never has two calls in flight: drops due meanwhile go together in the next, oldest first.
func TestAnOwnerHasOneCallInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}, gate: make(chan struct{})}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		s.until(s.delay)
		s.g.Roll(solo(trio, ayla))
		s.until(2*s.delay + 3)
		if n := len(items.made()); n != 1 {
			t.Fatalf("%d calls with the first in flight, want 1", n)
		}
		close(items.gate)
		synctest.Wait()
		s.step()
		calls := items.made()
		if len(calls) != 2 || len(calls[1].Drops) != 3 || calls[1].Drops[0].Template != "slime_gel" || calls[1].Drops[2].Template != "mushroom_cap" {
			t.Fatalf("calls %+v; want the trio's three together once the first was answered", calls)
		}
		if !slices.Equal(s.batches.sizes, []int{1, 3}) {
			t.Fatalf("batch sizes %v", s.batches.sizes)
		}
	})
}

func TestACallCarriesAtMostTheBatchMax(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := sceneWith(t, items, options{batchMax: 2})
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.g.Roll(solo(slime, ayla))
		s.until(s.delay + 2)
		calls := items.made()
		if len(calls) != 2 || len(calls[0].Drops) != 2 || len(calls[1].Drops) != 2 {
			t.Fatalf("calls %+v; want two of two", calls)
		}
		var templates []string
		for _, c := range calls {
			for _, d := range c.Drops {
				templates = append(templates, d.Template)
			}
		}
		if !slices.Equal(templates, []string{"slime_gel", "red_potion", "mushroom_cap", "slime_gel"}) {
			t.Fatalf("granted %v; want the oldest first", templates)
		}
	})
}

func TestEachOwnerHasTheirOwnCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla, bo := owner("Ayla"), owner("Bo")
		s.g.Roll(partyKill(trio, ayla, partyOf("p1", ayla, bo), ayla, bo))
		s.until(s.delay)
		calls := items.made()
		if len(calls) != 2 {
			t.Fatalf("%d calls, want one per owner", len(calls))
		}
		by := map[kernel.CharacterID]int{}
		for _, c := range calls {
			by[c.Character] = len(c.Drops)
		}
		if by[ayla.ID] != 2 || by[bo.ID] != 1 {
			t.Fatalf("drops per owner %v", by)
		}
	})
}

// A grant call is its own trace: a root, a consumer of the job the tick handed over, naming the owner and its drops, with the item call inside it.
func TestAGrantCallKeepsItsTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.until(s.delay)
		ended := s.spans.Ended()
		if len(ended) != 1 || ended[0].Name() != "world.collect" || ended[0].Parent().IsValid() || ended[0].SpanKind() != trace.SpanKindConsumer {
			t.Fatalf("%d spans; want one world.collect root, a consumer", len(ended))
		}
		attrs := attribute.NewSet(ended[0].Attributes()...)
		character, _ := attrs.Value("character_id")
		drops, _ := attrs.Value("drops")
		if character.AsString() != ayla.ID.String() || drops.AsInt64() != 3 {
			t.Fatalf("the root names character %q and %d drops", character.AsString(), drops.AsInt64())
		}
		if items.spanOf(0).SpanID() != ended[0].SpanContext().SpanID() {
			t.Fatal("the item call ran outside the grant's span")
		}
		if !loot.EntryPoint("world.collect") || loot.EntryPoint("world.tick") {
			t.Fatal("the grant's span does not start a kept trace")
		}
	})
}

// An answer that misses some of its drops is a failed call: they all go again under their keys.
func TestAnAnswerMissingDropsFailsTheCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		short := func(int, model.GrantCall) (model.Granted, error) { return model.Granted{}, nil }
		items := &grants{answers: []answer{short, every(model.GrantAdded)}}
		s := newScene(t, items)
		s.g.Roll(solo(slime, owner("Ayla")))
		if settled := s.until(s.delay + 1); lootIn(settled) != nil || len(s.g.Views()) != 1 || !strings.Contains(s.logs.String(), "answered 0 of 1 drops") {
			t.Fatalf("loot %v, logs %q; want the drop kept and the short answer logged", lootIn(settled), s.logs.String())
		}
		if got := kinds(lootIn(s.until(s.tick + s.roomCheck + 1))); !slices.Equal(got, []model.LootKind{model.LootCollected}) {
			t.Fatalf("loot %v, want the gel collected on the next try", got)
		}
	})
}
