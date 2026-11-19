package loot_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/application/world/itemcalls"
	"finga/internal/application/world/loot"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/transient"
)

const tick = 50 * time.Millisecond

// A slime that always drops two gels; a trio that always drops three things; an elite slime.
var (
	slime = gamedata.Mob{ID: "green_slime", Drops: []gamedata.Drop{{Item: "slime_gel", Chance: 1, Min: 2, Max: 2}}}
	trio  = gamedata.Mob{ID: "trio", Drops: []gamedata.Drop{
		{Item: "slime_gel", Chance: 1, Min: 1, Max: 1}, {Item: "red_potion", Chance: 1, Min: 1, Max: 1}, {Item: "mushroom_cap", Chance: 1, Min: 1, Max: 1},
	}}
	eliteSlime = gamedata.Mob{ID: "elite_slime", Elite: true, Drops: slime.Drops}
	// A boar that always drops an iron sword, whose attack rolls in 16 to 20 and nothing else.
	boar = gamedata.Mob{ID: "boar", Drops: []gamedata.Drop{{Item: "iron_sword", Chance: 1, Min: 1, Max: 1}}}
)

var errDown = errors.New("item module down")

// answer is how the item module answers one call.
type answer func(n int, c model.GrantCall) (model.Granted, error)

// every answers each item of a call the same way; added ones each fill a slot of their own, at the call's number as the revision.
func every(kind model.GrantKind) answer {
	return func(n int, c model.GrantCall) (model.Granted, error) {
		kinds := make([]model.GrantKind, len(c.Drops))
		for k := range kinds {
			kinds[k] = kind
		}
		return outcomes(kinds...)(n, c)
	}
}

// outcomes answers each item of a call in turn.
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

// grants is the item module: each call takes the next answer, the last one again once they run out.
type grants struct {
	mu      sync.Mutex
	answers []answer
	calls   []model.GrantCall
	spans   []trace.SpanContext                    // each call's
	gate    chan struct{}                          // when set, each call waits for it to close
	hold    func(ctx context.Context, n int) error // when set, the n-th call runs it first, failing with what it returns
}

func (f *grants) AddMany(ctx context.Context, c model.GrantCall) (model.Granted, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.spans = append(f.spans, trace.SpanContextFromContext(ctx))
	n := len(f.calls)
	a, gate, hold := f.answers[min(n, len(f.answers))-1], f.gate, f.hold
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if hold != nil {
		if err := hold(ctx, n); err != nil {
			return model.Granted{}, err
		}
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

// options shape a scene: the map's random source, the items per call, whether the workers start, the ground, and the quests' items.
type options struct {
	roll     model.Roll
	batchMax int
	idle     bool
	rest     func(x, z, y float64) (float64, bool) // the ground; flat and open everywhere by default
	collects func(owner kernel.CharacterID, item string) bool
}

// scene is a map's ground on its own: each step is a tick that settles the answers back, then steps the ground, and lets the workers answer.
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
	data    *gamedata.Data
	// The lives and the retry in ticks.
	pouchLife, boxLife, retry uint64
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
	if o.rest == nil {
		o.rest = func(_, _, _ float64) (float64, bool) { return 0, true }
	}
	if o.collects == nil {
		o.collects = func(kernel.CharacterID, string) bool { return false }
	}
	data := worldtest.LoadData(t)
	ticks := func(d time.Duration) uint64 { return model.TicksIn(d, tick) }
	s := &scene{
		t: t, items: items, results: make(chan loot.Result, 64), batches: &batches{}, logs: &syncWriter{}, spans: tracetest.NewSpanRecorder(), data: data,
		workers:   itemcalls.NewPool(itemcalls.Deps{Observer: jobs{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2}),
		pouchLife: ticks(data.Loot().PouchLife), boxLife: ticks(data.Loot().BoxLife), retry: ticks(2 * time.Second),
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
		MapID: "forest", Items: items, ItemCalls: s.workers, Observer: s.batches, Traces: traces, Roll: o.roll, Data: data, Rest: o.rest, Collects: o.collects,
		Logger: slog.New(slog.NewTextHandler(s.logs, nil)), Settle: func(_ context.Context, r loot.Result) { s.results <- r },
	}, loot.Tuning{TickInterval: tick, BatchMax: o.batchMax})
	s.g.Begin(0)
	return s
}

// step runs one tick: the answers back settle, then the ground steps; the workers answer before it returns.
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
	out = append(out, s.g.Step()...)
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

// here is a character's middle standing at the kill, 0.9 m over the ground.
var here = loot.Point{X: killX, Y: 0.9, Z: killZ}

// answered is what a take heard, once it did.
type answered struct {
	got  []model.PickAnswer
	done bool
}

// takeFrom asks for picks from at; the workers have the call before it returns.
func (s *scene) takeFrom(o loot.Owner, at loot.Point, picks ...model.Pick) *answered {
	a := &answered{}
	s.g.Take(o, at, picks, func(got []model.PickAnswer) { a.got, a.done = got, true })
	synctest.Wait()
	return a
}

func (s *scene) take(o loot.Owner, picks ...model.Pick) *answered {
	return s.takeFrom(o, here, picks...)
}

// errs are the take's answers' errors, in its order.
func (a *answered) errs() []error {
	out := make([]error, len(a.got))
	for k, p := range a.got {
		out[k] = p.Err
	}
	return out
}

// all are the picks of every item in the pouch.
func all(p model.PouchView) []model.Pick {
	var out []model.Pick
	for _, it := range p.Items {
		out = append(out, model.Pick{Pouch: p.ID, Item: it.ID})
	}
	return out
}

// only is o's one pouch on the ground.
func (s *scene) only(o loot.Owner) model.PouchView {
	s.t.Helper()
	ps := s.g.Views(o.ID)
	if len(ps) != 1 {
		s.t.Fatalf("%s has %d pouches, want one", o.Name, len(ps))
	}
	return ps[0]
}

// templates are the pouch's items' templates, in order.
func templates(p model.PouchView) []string {
	var out []string
	for _, it := range p.Items {
		out = append(out, it.Template)
	}
	return out
}

func owner(name string) loot.Owner {
	return loot.Owner{ID: kernel.NewCharacterID(), Account: kernel.NewAccountID(), Name: name}
}

// Where the test kills happen.
const killX, killZ = 6.25, 12.5

// solo is a kill by o alone.
func solo(mob gamedata.Mob, o loot.Owner) loot.Kill {
	return loot.Kill{Mob: mob, X: killX, Z: killZ, Killer: o, Eligible: []loot.Owner{o}}
}

// partyKill is a kill by killer in party, whose eligible members are those given.
func partyKill(mob gamedata.Mob, killer loot.Owner, party model.Party, eligible ...loot.Owner) loot.Kill {
	return loot.Kill{Mob: mob, X: killX, Z: killZ, Killer: killer, Party: party, Eligible: eligible}
}

func partyOf(id string, members ...loot.Owner) model.Party {
	p := model.Party{ID: id}
	for _, m := range members {
		p.Members = append(p.Members, m.ID)
	}
	return p
}

// owners are the owners of the pouches on the ground among those given, oldest pouch first.
func owners(g *loot.Ground, of ...loot.Owner) []kernel.CharacterID {
	by := map[uint64]kernel.CharacterID{}
	for _, o := range of {
		for _, p := range g.Views(o.ID) {
			by[p.ID] = o.ID
		}
	}
	var out []kernel.CharacterID
	for _, id := range slices.Sorted(maps.Keys(by)) {
		out = append(out, by[id])
	}
	return out
}

// lootIn is every Loot in what settled, in order.
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

func names(items []model.LootItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Template)
	}
	return out
}

func TestAKillLeavesAPouchByItsChance(t *testing.T) {
	chance := worldtest.LoadData(t).Loot().Pouch
	for _, tt := range []struct {
		name          string
		roll          float64
		normal, elite bool
	}{
		{"a roll under the normal chance leaves both a pouch", chance.Normal - 0.01, true, true},
		{"a roll between the two, the elite alone", (chance.Normal + chance.Elite) / 2, false, true},
		{"a roll at the elite chance, neither", chance.Elite, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: worldtest.FixedRoll{V: tt.roll}})
				ayla, bo := owner("Ayla"), owner("Bo")
				s.g.Roll(solo(slime, ayla))
				s.g.Roll(solo(eliteSlime, bo))
				if got := []bool{len(s.g.Views(ayla.ID)) == 1, len(s.g.Views(bo.ID)) == 1}; !slices.Equal(got, []bool{tt.normal, tt.elite}) {
					t.Fatalf("a pouch for the normal kill and the elite's: %v, want %v", got, []bool{tt.normal, tt.elite})
				}
			})
		})
	}
}

// A kill whose pouch chance fails still rolls what an active quest of its owner's collects, so a quest takes no more kills; with nothing of that, no pouch falls, and the party's turn stays.
func TestAnActiveQuestsItemComesOutsideTheChance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ayla, bo := owner("Ayla"), owner("Bo")
		collects := func(o kernel.CharacterID, item string) bool { return o == bo.ID && item == "red_potion" }
		s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: worldtest.FixedRoll{V: 0.99}, collects: collects})
		party := partyOf("p1", ayla, bo)
		s.g.Roll(partyKill(trio, ayla, party, ayla, bo))
		s.g.Roll(partyKill(trio, ayla, party, ayla, bo))
		if got := owners(s.g, ayla, bo); len(got) != 0 {
			t.Fatalf("owners %v; want no pouch, the turn staying on Ayla, who collects nothing", got)
		}
		s.g.Roll(solo(trio, bo))
		if got := templates(s.only(bo)); !slices.Equal(got, []string{"red_potion"}) {
			t.Fatalf("Bo's pouch holds %v, want only the potion his quest collects", got)
		}
	})
}

// One pouch falls where the mob died, on the ground there, holding the whole roll; its items are numbered within it.
func TestAKillLeavesOnePouchHoldingItsWholeRoll(t *testing.T) {
	for _, tt := range []struct {
		name  string
		rest  func(x, z, y float64) (float64, bool)
		wantY float64
	}{
		{"on the ground under the kill", func(_, _, _ float64) (float64, bool) { return 1.5, true }, 1.5},
		{"where the mob died, with no ground found", func(_, _, _ float64) (float64, bool) { return 0, false }, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{rest: tt.rest})
				ayla := owner("Ayla")
				k := solo(trio, ayla)
				k.Y = 3
				s.g.Roll(k)
				p := s.only(ayla)
				if p.Kind != model.PouchSmall || p.X != killX || p.Y != tt.wantY || p.Z != killZ || p.GoneIn != s.data.Loot().PouchLife {
					t.Fatalf("pouch %+v; want a small pouch at (%v, %v, %v) for its whole life", p, killX, tt.wantY, killZ)
				}
				if !slices.Equal(templates(p), []string{"slime_gel", "red_potion", "mushroom_cap"}) || p.Items[0].ID != 1 || p.Items[2].ID != 3 {
					t.Fatalf("items %+v; want the trio's three, numbered 1 to 3", p.Items)
				}
			})
		})
	}
}

func TestAPartyTakesItsPouchesInTurn(t *testing.T) {
	ann, bo, cy, dee := owner("Ann"), owner("Bo"), owner("Cy"), owner("Dee")
	everyone := []loot.Owner{ann, bo, cy, dee}
	all := partyOf("p1", ann, bo, cy)
	tests := []struct {
		name  string
		kills func(s *scene)
		want  []loot.Owner
	}{
		{"each kill's pouch goes to the next member, from the first in join order, the whole roll in it", func(s *scene) {
			for range 3 {
				s.g.Roll(partyKill(trio, bo, all, ann, bo, cy))
			}
		}, []loot.Owner{ann, bo, cy}},
		{"the turn carries over from kill to kill, and wraps", func(s *scene) {
			for _, killer := range []loot.Owner{cy, cy, ann, bo} {
				s.g.Roll(partyKill(slime, killer, all, ann, bo, cy))
			}
		}, []loot.Owner{ann, bo, cy, ann}},
		{"a member not eligible, dead or on another map, is skipped", func(s *scene) {
			for range 3 {
				s.g.Roll(partyKill(trio, ann, all, ann, cy))
			}
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
			for range 3 {
				s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			}
			grown := partyOf("p1", ann, bo, cy, dee)
			s.g.Roll(partyKill(slime, ann, grown, ann, bo, cy, dee))
		}, []loot.Owner{ann, bo, cy, dee}},
		{"each party keeps its own turn", func(s *scene) {
			other := partyOf("p2", dee, cy)
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann, bo))
			s.g.Roll(partyKill(slime, dee, other, dee, cy))
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann, bo))
		}, []loot.Owner{ann, dee, bo}},
		{"a turn unused for ten minutes starts again from the first member, the first pouch's life over by then", func(s *scene) {
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
			s.tick += uint64(10*time.Minute/tick) + 1
			s.g.Begin(s.tick)
			s.g.Step()
			s.g.Roll(partyKill(slime, ann, all, ann, bo, cy))
		}, []loot.Owner{ann}},
		{"without a party, or with nobody eligible, it's the killer's", func(s *scene) {
			s.g.Roll(solo(slime, ann))
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo), ann))
			s.g.Roll(partyKill(slime, ann, partyOf("p1", ann, bo)))
		}, []loot.Owner{ann, ann, ann}},
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
				if got := owners(s.g, everyone...); !slices.Equal(got, want) {
					t.Fatalf("owners %v, want %v", got, want)
				}
			})
		})
	}
}

// A boss leaves each player it's given a box of their own: its whole roll, never gated, for a box's life.
func TestABossLeavesABoxForEachPlayerItsGiven(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: worldtest.FixedRoll{V: 0.99}})
		ann, bo, cy := owner("Ann"), owner("Bo"), owner("Cy")
		s.g.Box(trio, killX, 0, killZ, ann)
		s.g.Box(trio, killX, 0, killZ, bo)
		for _, o := range []loot.Owner{ann, bo} {
			p := s.only(o)
			if p.Kind != model.PouchBox || len(p.Items) != 3 || p.GoneIn != s.data.Loot().BoxLife {
				t.Fatalf("%s's box %+v; want a box of the trio's three, for a box's life", o.Name, p)
			}
		}
		if got := s.g.Views(cy.ID); len(got) != 0 {
			t.Fatalf("Cy sees %v; a box is only its owner's", got)
		}
	})
}

// A pouch is told to its owner alone, in the tick it fell, and each later change in the tick it happens.
func TestAPouchIsToldOnlyToItsOwner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{outcomes(model.GrantAdded)}})
		ayla, bo := owner("Ayla"), owner("Bo")
		s.g.Roll(solo(trio, ayla))
		p := s.only(ayla)
		if c := s.g.Changes(ayla.ID); len(c.Changed) != 1 || c.Changed[0].ID != p.ID || len(c.Changed[0].Items) != 3 {
			t.Fatalf("Ayla's changes %+v, want her new pouch", c)
		}
		if c := s.g.Changes(bo.ID); len(c.Changed) != 0 || len(c.Gone) != 0 {
			t.Fatalf("Bo's changes %+v, want none", c)
		}
		s.step()
		if c := s.g.Changes(ayla.ID); len(c.Changed) != 0 {
			t.Fatalf("changes %+v on the next tick, want none", c)
		}
		s.take(ayla, model.Pick{Pouch: p.ID, Item: 1})
		s.step()
		if c := s.g.Changes(ayla.ID); len(c.Changed) != 1 || !slices.Equal(templates(c.Changed[0]), []string{"red_potion", "mushroom_cap"}) {
			t.Fatalf("changes %+v once the gel was taken, want the pouch with the other two", c)
		}
		if c := s.g.Changes(bo.ID); len(c.Changed) != 0 {
			t.Fatalf("Bo's changes %+v, want none", c)
		}
		s.g.Roll(solo(slime, bo))
		if va, vb := s.g.Views(ayla.ID), s.g.Views(bo.ID); len(va) != 1 || va[0].ID != p.ID || len(vb) != 1 || vb[0].ID == p.ID {
			t.Fatalf("views %+v and %+v; want each their own pouch alone, as a join shows it", va, vb)
		}
	})
}

func TestATakeGrantsThePicksAndAnEmptiedPouchGoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		p := s.only(ayla)
		a := s.take(ayla, all(p)...)
		calls := items.made()
		if len(calls) != 1 || len(calls[0].Drops) != 3 || calls[0].Character != ayla.ID || calls[0].Account != ayla.Account {
			t.Fatalf("calls %+v; want one of the three for Ayla", calls)
		}
		settled := s.step()
		if !a.done || slices.ContainsFunc(a.errs(), func(err error) bool { return err != nil }) {
			t.Fatalf("answers %+v; want all three taken", a)
		}
		ls := lootIn(settled)
		if len(ls) != 1 || ls[0].Kind != model.LootTaken || ls[0].Pouch != p.ID || ls[0].Owner != ayla.ID || ls[0].OwnerName != "Ayla" || ls[0].MapID != "forest" ||
			!slices.Equal(names(ls[0].Items), []string{"slime_gel", "red_potion", "mushroom_cap"}) {
			t.Fatalf("loot %+v; want one line of the three taken", ls)
		}
		if len(settled[0].Inventory) != 1 || settled[0].Owner.ID != ayla.ID {
			t.Fatalf("settled %+v; want Ayla's inventory change", settled)
		}
		if c := s.g.Changes(ayla.ID); !slices.Equal(c.Gone, []uint64{p.ID}) || len(c.Changed) != 0 || len(s.g.Views(ayla.ID)) != 0 {
			t.Fatalf("changes %+v; want the emptied pouch gone at once", c)
		}
	})
}

// Every way a pick is refused, each pick on its own: the rest of the take goes on.
func TestEachRefusedPick(t *testing.T) {
	reach := worldtest.LoadData(t).Loot().Reach
	for _, tt := range []struct {
		name string
		take func(s *scene, ayla, bo loot.Owner, p model.PouchView) *answered
		want []error
	}{
		{"a pouch that never was", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.take(ayla, model.Pick{Pouch: p.ID + 9, Item: 1}, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchGone, nil}},
		{"another player's pouch", func(s *scene, _, bo loot.Owner, p model.PouchView) *answered {
			return s.take(bo, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchGone}},
		{"an item the pouch doesn't hold", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.take(ayla, model.Pick{Pouch: p.ID, Item: 7}, model.Pick{Pouch: p.ID, Item: 2})
		}, []error{model.ErrPouchGone, nil}},
		{"the same item twice in one take", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.take(ayla, model.Pick{Pouch: p.ID, Item: 1}, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{nil, model.ErrPouchGone}},
		{"an item refused, named again in the take", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.takeFrom(ayla, loot.Point{X: killX, Y: reach + 0.6, Z: killZ}, model.Pick{Pouch: p.ID, Item: 1}, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchOutOfReach, model.ErrPouchGone}},
		{"an item already taken", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			s.take(ayla, model.Pick{Pouch: p.ID, Item: 1})
			s.step()
			return s.take(ayla, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchGone}},
		{"a pouch whose life ended", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			s.until(s.pouchLife)
			return s.take(ayla, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchGone}},
		{"a pouch past reach and its half-metre slack, from the character's middle", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.takeFrom(ayla, loot.Point{X: killX, Y: reach + 0.6, Z: killZ}, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{model.ErrPouchOutOfReach}},
		{"a pouch within the slack", func(s *scene, ayla, _ loot.Owner, p model.PouchView) *answered {
			return s.takeFrom(ayla, loot.Point{X: killX + reach + 0.4, Y: 0, Z: killZ}, model.Pick{Pouch: p.ID, Item: 1})
		}, []error{nil}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
				ayla, bo := owner("Ayla"), owner("Bo")
				s.g.Roll(solo(trio, ayla))
				s.g.Roll(solo(slime, bo))
				a := tt.take(s, ayla, bo, s.g.Views(ayla.ID)[0])
				s.step()
				if !a.done || len(a.got) != len(tt.want) {
					t.Fatalf("answers %+v, want %d", a, len(tt.want))
				}
				for k, err := range a.errs() {
					if (err == nil) != (tt.want[k] == nil) || tt.want[k] != nil && !errors.Is(err, tt.want[k]) {
						t.Fatalf("pick %d answered %v, want %v", k, err, tt.want[k])
					}
				}
			})
		})
	}
}

// An item whose tab is full stays in its pouch, answered with that tab; the rest of the take is taken, and the item can be taken once there's room.
func TestAFullTabLeavesTheItemInItsPouch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{outcomes(model.GrantAdded, model.GrantNoRoom, model.GrantAdded), every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		p := s.only(ayla)
		a := s.take(ayla, all(p)...)
		settled := s.step()
		if errs := a.errs(); errs[0] != nil || !errors.Is(errs[1], kernel.ErrUseTabFull) || errs[2] != nil {
			t.Fatalf("answers %v; want the potion refused for its Use tab, the rest taken", errs)
		}
		if ls := lootIn(settled); len(ls) != 1 || !slices.Equal(names(ls[0].Items), []string{"slime_gel", "mushroom_cap"}) {
			t.Fatalf("loot %+v; want the gel and cap taken", ls)
		}
		if got := templates(s.only(ayla)); !slices.Equal(got, []string{"red_potion"}) {
			t.Fatalf("the pouch holds %v, want the potion left in it", got)
		}
		a = s.take(ayla, model.Pick{Pouch: p.ID, Item: 2})
		s.step()
		if !a.done || a.got[0].Err != nil || len(s.g.Views(ayla.ID)) != 0 {
			t.Fatalf("answer %+v; want the potion taken once there was room, and the pouch gone", a.got)
		}
	})
}

// A call with no answer leaves its items taken, asked again under the same keys until one comes; nothing is given twice.
func TestAnUnansweredCallIsAskedAgainUnderTheSameKeys(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{down, down, every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		p := s.only(ayla)
		a := s.take(ayla, all(p)...)
		if settled := s.step(); lootIn(settled) != nil || !a.done || !errors.Is(a.got[0].Err, transient.ErrUnavailable) {
			t.Fatalf("loot %v, answer %+v; want nothing taken and the pick answered as unavailable", lootIn(settled), a.got)
		}
		if again := s.take(ayla, all(p)...); !again.done || !errors.Is(again.got[0].Err, model.ErrTakeBusy) {
			t.Fatalf("a take of the item asked again answered %+v; want it refused as being taken", again.got)
		}
		settled := s.until(s.tick + s.retry + 1)
		calls := items.made()
		if len(calls) != 3 || calls[0].Drops[0].Key != calls[2].Drops[0].Key {
			t.Fatalf("calls %+v; want the gel asked again under its key", calls)
		}
		if got := kinds(lootIn(settled)); !slices.Equal(got, []model.LootKind{model.LootTaken}) || len(s.g.Views(ayla.ID)) != 0 {
			t.Fatalf("loot %v; want the gel taken once, and the pouch gone", got)
		}
	})
}

// A grant call with no answer is cut off 3.5 s in, its retry included, so a take answers each pick unavailable inside the 5 s the gateway waits rather than the gateway giving up on the whole request.
func TestATakeWithNoAnswerIsAnsweredInsideTheGatewaysWait(t *testing.T) {
	for _, tt := range []struct {
		name  string
		hold  func(ctx context.Context, n int) error
		calls int
	}{
		{"a call that hangs", func(ctx context.Context, _ int) error { <-ctx.Done(); return ctx.Err() }, 1},
		{"a retry after a slow failure", func(ctx context.Context, n int) error {
			if n == 1 {
				time.Sleep(3 * time.Second)
				return errDown
			}
			<-ctx.Done()
			return ctx.Err()
		}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				items := &grants{answers: []answer{every(model.GrantAdded)}, hold: tt.hold}
				s := newScene(t, items)
				ayla := owner("Ayla")
				s.g.Roll(solo(slime, ayla))
				p := s.only(ayla)
				began := time.Now()
				a := s.take(ayla, all(p)...)
				for !a.done && time.Since(began) < 10*time.Second {
					time.Sleep(tick)
					s.step()
				}
				took := time.Since(began)
				if !a.done || !errors.Is(a.got[0].Err, transient.ErrUnavailable) || took < 3500*time.Millisecond || took > 3500*time.Millisecond+2*tick {
					t.Fatalf("answered %v after %v with %+v; want each pick unavailable at 3.5 s", a.done, took, a.got)
				}
				if n := len(items.made()); n != tt.calls {
					t.Fatalf("%d calls in the take's time, want %d", n, tt.calls)
				}
			})
		})
	}
}

// An item still unanswered when its pouch's life ends has one more try; with no answer then, it's given up and logged.
func TestAnItemUnansweredPastItsPouchsLifeIsGivenUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{down}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		s.take(ayla, all(s.only(ayla))...)
		settled := s.until(s.pouchLife + 1)
		if lootIn(settled) != nil || len(s.g.Views(ayla.ID)) != 0 {
			t.Fatalf("loot %v; want the pouch gone from view, nothing said left of the item in a call", lootIn(settled))
		}
		calls := len(items.made())
		s.until(s.tick + s.retry + 1)
		if got := len(items.made()); got != calls+2 || !strings.Contains(s.logs.String(), "given up") {
			t.Fatalf("%d calls, then %d; logs %q; want one more try past its life, then the item given up", calls, got, s.logs.String())
		}
		s.until(s.tick + 3*s.retry)
		if got := len(items.made()); got != calls+2 {
			t.Fatalf("%d calls after giving up, want no more", got)
		}
	})
}

// An owner has one call in flight and one take waiting: a third take is busy, unless it only picks from the waiting take's pouches, which it joins.
func TestAnOwnerHasOneCallInFlightAndOneTakeWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}, gate: make(chan struct{})}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.g.Roll(solo(slime, ayla))
		first, second := s.g.Views(ayla.ID)[0], s.g.Views(ayla.ID)[1]
		inFlight := s.take(ayla, model.Pick{Pouch: first.ID, Item: 1})
		waiting := s.take(ayla, model.Pick{Pouch: first.ID, Item: 2})
		busy := s.take(ayla, model.Pick{Pouch: second.ID, Item: 1})
		joins := s.take(ayla, model.Pick{Pouch: first.ID, Item: 3})
		taken := s.take(ayla, model.Pick{Pouch: first.ID, Item: 2})
		if !busy.done || !errors.Is(busy.got[0].Err, model.ErrTakeBusy) || !taken.done || !errors.Is(taken.got[0].Err, model.ErrTakeBusy) {
			t.Fatalf("a take of another pouch answered %+v, one of an item waiting %+v; want both busy", busy.got, taken.got)
		}
		if inFlight.done || waiting.done || joins.done || len(items.made()) != 1 {
			t.Fatalf("%d calls; want the first in flight, the rest waiting for it", len(items.made()))
		}
		close(items.gate)
		synctest.Wait()
		s.step()
		if !inFlight.done || waiting.done || joins.done {
			t.Fatal("want the first take answered once its call settled, the waiting ones sent next")
		}
		s.step()
		calls := items.made()
		if !waiting.done || !joins.done || waiting.got[0].Err != nil || joins.got[0].Err != nil || len(calls) != 2 ||
			calls[1].Drops[0].Template != "red_potion" || calls[1].Drops[1].Template != "mushroom_cap" || !slices.Equal(s.batches.sizes, []int{1, 2}) {
			t.Fatalf("calls %+v, batches %v; want the waiting take and the one that joined it in one call, both taken", calls, s.batches.sizes)
		}
	})
}

// Takes pressed over and over grant each item once: every take of an item being taken is refused as busy.
func TestSpammedTakesGrantEachItemOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}, gate: make(chan struct{})}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		picks := all(s.only(ayla))
		var spam []*answered
		for range 20 {
			spam = append(spam, s.take(ayla, picks...))
		}
		close(items.gate)
		synctest.Wait()
		settled := s.until(s.tick + 3)
		granted := 0
		for _, c := range items.made() {
			granted += len(c.Drops)
		}
		if granted != 3 || len(lootIn(settled)) != 1 {
			t.Fatalf("%d items granted, loot %v; want each of the three once", granted, lootIn(settled))
		}
		for _, a := range spam[1:] {
			for _, err := range a.errs() {
				if !errors.Is(err, model.ErrTakeBusy) {
					t.Fatalf("a repeated take answered %v, want busy", a.errs())
				}
			}
		}
	})
}

func TestACallCarriesAtMostTheBatchMax(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := sceneWith(t, items, options{batchMax: 2})
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		a := s.take(ayla, all(s.only(ayla))...)
		s.step()
		if calls := items.made(); len(calls) != 1 || len(calls[0].Drops) != 2 {
			t.Fatalf("calls %+v; want one of two", calls)
		}
		if errs := a.errs(); errs[0] != nil || errs[1] != nil || !errors.Is(errs[2], model.ErrTakeBusy) {
			t.Fatalf("answers %v; want the first two taken and the third left for the next take", errs)
		}
	})
}

// A player has at most the most pouches and boxes on a map: a new one ends the oldest's life at once, with what was left in it; another player's count apart.
func TestANewPouchPastTheMostEndsTheOldest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
		ayla, bo := owner("Ayla"), owner("Bo")
		most := s.data.Loot().PouchesMax
		for range most {
			s.g.Roll(solo(slime, ayla))
		}
		s.g.Roll(solo(slime, bo))
		if settled := s.step(); lootIn(settled) != nil || len(s.g.Views(ayla.ID)) != most {
			t.Fatalf("loot %v, %d pouches; want all %d kept", lootIn(settled), len(s.g.Views(ayla.ID)), most)
		}
		oldest := s.g.Views(ayla.ID)[0]
		s.g.Box(trio, killX, 0, killZ, ayla)
		settled := s.step()
		left := lootIn(settled)
		if len(left) != 1 || left[0].Kind != model.LootLeft || left[0].Pouch != oldest.ID || !slices.Equal(names(left[0].Items), []string{"slime_gel"}) {
			t.Fatalf("loot %+v; want the oldest pouch left with its gels", left)
		}
		views := s.g.Views(ayla.ID)
		if len(views) != most || views[0].ID == oldest.ID || views[len(views)-1].Kind != model.PouchBox || len(s.g.Views(bo.ID)) != 1 {
			t.Fatalf("%d pouches, first %d, last %+v; Bo's %d; want the most, the oldest gone for the box, and Bo's kept", len(views), views[0].ID, views[len(views)-1], len(s.g.Views(bo.ID)))
		}
	})
}

// A pouch's life ends with what it still holds, which its owner hears was left; a box lasts longer.
func TestALifeEndsWithWhatWasLeft(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.g.Box(slime, killX, 0, killZ, ayla)
		pouch := s.g.Views(ayla.ID)[0]
		if settled := s.until(s.pouchLife - 1); lootIn(settled) != nil {
			t.Fatalf("loot %v before the pouch's life ended", lootIn(settled))
		}
		settled := s.step()
		ls := lootIn(settled)
		if len(ls) != 1 || ls[0].Kind != model.LootLeft || ls[0].Pouch != pouch.ID || settled[0].Owner.ID != ayla.ID ||
			!slices.Equal(names(ls[0].Items), []string{"slime_gel", "red_potion", "mushroom_cap"}) {
			t.Fatalf("loot %+v; want the pouch's three said left", ls)
		}
		if c := s.g.Changes(ayla.ID); !slices.Equal(c.Gone, []uint64{pouch.ID}) {
			t.Fatalf("changes %+v, want the pouch gone", c)
		}
		if ps := s.g.Views(ayla.ID); len(ps) != 1 || ps[0].Kind != model.PouchBox {
			t.Fatalf("pouches %+v, want the box still there", ps)
		}
		if ls := lootIn(s.until(s.boxLife)); len(ls) != 1 || ls[0].Kind != model.LootLeft || len(s.g.Views(ayla.ID)) != 0 {
			t.Fatalf("loot %+v; want the box's gel left at its life's end", ls)
		}
	})
}

// An item in a call when its pouch's life ends isn't left: it's taken once its answer comes.
func TestAnItemBeingTakenOutlivesItsPouch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}, gate: make(chan struct{})}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		a := s.take(ayla, model.Pick{Pouch: s.only(ayla).ID, Item: 1})
		ls := lootIn(s.until(s.pouchLife))
		if len(ls) != 1 || ls[0].Kind != model.LootLeft || !slices.Equal(names(ls[0].Items), []string{"red_potion", "mushroom_cap"}) {
			t.Fatalf("loot %+v; want the two not being taken said left", ls)
		}
		close(items.gate)
		synctest.Wait()
		ls = lootIn(s.step())
		if !a.done || a.got[0].Err != nil || len(ls) != 1 || ls[0].Kind != model.LootTaken || !slices.Equal(names(ls[0].Items), []string{"slime_gel"}) {
			t.Fatalf("answer %+v, loot %+v; want the gel taken", a.got, ls)
		}
	})
}

func TestARefusedItemIsGivenUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newScene(t, &grants{answers: []answer{outcomes(model.GrantRefused)}})
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		a := s.take(ayla, all(s.only(ayla))...)
		settled := s.step()
		if !a.done || !errors.Is(a.got[0].Err, model.ErrPouchGone) || lootIn(settled) != nil || len(s.g.Views(ayla.ID)) != 0 || !strings.Contains(s.logs.String(), "REASON_BAD_ROLL") {
			t.Fatalf("answer %+v, loot %v, logs %q; want the gel given up and logged, its pouch gone", a.got, lootIn(settled), s.logs.String())
		}
	})
}

func TestAnItemGrantedBeforeAnswersItsStoredReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		potion := model.Slot{Tab: gamedata.Use, Slot: 3, Item: &model.SlotItem{Template: "red_potion", Quantity: 1}}
		replayed := func(n int, c model.GrantCall) (model.Granted, error) {
			out, _ := outcomes(model.GrantAdded, model.GrantReplayed, model.GrantReplayed)(n, c)
			out.Outcomes[1].Replayed = model.Inventory{Revision: 4, Slots: []model.Slot{potion}}
			out.Added.Revision = 9
			return out, nil
		}
		s := newScene(t, &grants{answers: []answer{replayed}})
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.take(ayla, all(s.only(ayla))...)
		settled := s.step()
		if ls := lootIn(settled); len(ls) != 1 || len(ls[0].Items) != 3 {
			t.Fatalf("loot %+v, want all three taken", ls)
		}
		inv := settled[0].Inventory
		if len(inv) != 2 || inv[0].Revision != 4 || inv[0].Slots[0] != potion || inv[1].Revision != 9 {
			t.Fatalf("inventory changes %+v; want the stored reply at 4, then the call's at 9", inv)
		}
	})
}

// A retry of items whose call got no answer that finds every worker's queue full waits the interval again, not a tick.
func TestARetryWithTheQueueFullWaitsTheIntervalAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{down, down, every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		s.take(ayla, all(s.only(ayla))...)
		s.step()
		failed := s.tick
		release := make(chan struct{})
		block := func(ctx context.Context) {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		for range 2 { // the workers each hold a job, then the queue fills
			for s.workers.Submit(block) {
			}
			synctest.Wait()
		}
		s.until(failed + s.retry)
		close(release)
		synctest.Wait()
		s.until(failed + 2*s.retry - 1)
		if got := len(items.made()); got != 2 {
			t.Fatalf("%d calls before a second interval, want only the failed call's 2", got)
		}
		s.step()
		if got := len(items.made()); got != 3 {
			t.Fatalf("%d calls once it passed, want the gels asked again", got)
		}
	})
}

// With every worker's queue full, a take is answered at once to try again, and its items stay to take.
func TestATakeWithTheQueueFullIsAnsweredToTryAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{idle: true})
		for s.workers.Submit(func(context.Context) {}) {
		}
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		picks := all(s.only(ayla))
		if a := s.take(ayla, picks...); !a.done || !errors.Is(a.got[0].Err, transient.ErrUnavailable) || len(s.batches.sizes) != 0 {
			t.Fatalf("answer %+v, batches %v; want it answered to try again, nothing sent", a.got, s.batches.sizes)
		}
		s.run()
		synctest.Wait()
		a := s.take(ayla, picks...)
		if s.step(); !a.done || a.got[0].Err != nil {
			t.Fatalf("answer %+v, want the gel taken once the queue had room", a.got)
		}
	})
}

// A grant call is its own trace: a root, a consumer of the job the tick handed over, naming the owner and its items, with the item call inside it.
func TestAGrantCallKeepsItsTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &grants{answers: []answer{every(model.GrantAdded)}}
		s := newScene(t, items)
		ayla := owner("Ayla")
		s.g.Roll(solo(trio, ayla))
		s.take(ayla, all(s.only(ayla))...)
		ended := s.spans.Ended()
		if len(ended) != 1 || ended[0].Name() != "world.take" || ended[0].Parent().IsValid() || ended[0].SpanKind() != trace.SpanKindConsumer {
			t.Fatalf("%d spans; want one world.take root, a consumer", len(ended))
		}
		attrs := attribute.NewSet(ended[0].Attributes()...)
		character, _ := attrs.Value("character_id")
		n, _ := attrs.Value("items")
		if character.AsString() != ayla.ID.String() || n.AsInt64() != 3 {
			t.Fatalf("the root names character %q and %d items", character.AsString(), n.AsInt64())
		}
		if items.spanOf(0).SpanID() != ended[0].SpanContext().SpanID() {
			t.Fatal("the item call ran outside the grant's span")
		}
		if !loot.EntryPoint("world.take") || loot.EntryPoint("world.tick") {
			t.Fatal("the grant's span does not start a kept trace")
		}
	})
}

// An answer that misses some of its items is a failed call: they all go again under their keys.
func TestAnAnswerMissingItemsFailsTheCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		short := func(int, model.GrantCall) (model.Granted, error) { return model.Granted{}, nil }
		s := newScene(t, &grants{answers: []answer{short, every(model.GrantAdded)}})
		ayla := owner("Ayla")
		s.g.Roll(solo(slime, ayla))
		s.take(ayla, all(s.only(ayla))...)
		if settled := s.step(); lootIn(settled) != nil || len(s.g.Views(ayla.ID)) != 1 || !strings.Contains(s.logs.String(), "answered 0 of 1 items") {
			t.Fatalf("loot %v, logs %q; want the item kept and the short answer logged", lootIn(settled), s.logs.String())
		}
		if got := kinds(lootIn(s.until(s.tick + s.retry + 1))); !slices.Equal(got, []model.LootKind{model.LootTaken}) {
			t.Fatalf("loot %v, want the gel taken on the next try", got)
		}
	})
}

// Equipment rolls its stats as it falls, from the map's random source; the pouch shows the roll and its grant carries it; anything else has none.
func TestEquipmentRollsItsStatsAsItFalls(t *testing.T) {
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
				ayla := owner("Ayla")
				s.g.Box(boar, killX, 0, killZ, ayla)
				want := progression.ItemStats{PhysicalAttack: tt.attack}
				if st := s.only(ayla).Items[0].Stats; st == nil || *st != want {
					t.Fatalf("the box shows %+v, want a sword of attack %d", st, tt.attack)
				}
				s.take(ayla, all(s.only(ayla))...)
				if got := s.items.made(); len(got) != 1 || got[0].Drops[0].Stats == nil || *got[0].Drops[0].Stats != want {
					t.Fatalf("granted %+v, want a sword of attack %d", got, tt.attack)
				}
			})
		})
	}
	t.Run("a gel has no roll", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := newScene(t, &grants{answers: []answer{every(model.GrantAdded)}})
			ayla := owner("Ayla")
			s.g.Roll(solo(slime, ayla))
			s.take(ayla, all(s.only(ayla))...)
			if got := s.items.made(); len(got) != 1 || got[0].Drops[0].Stats != nil {
				t.Fatalf("granted %+v, want no roll", got)
			}
		})
	})
}

// Rolls come from the map's seeded source, so a recorded fight replays the same loot.
func TestTheSameSeedRollsTheSameStats(t *testing.T) {
	const boxes = 5
	rolled := func(t *testing.T) []progression.ItemStats {
		t.Helper()
		var out []progression.ItemStats
		synctest.Test(t, func(t *testing.T) {
			s := sceneWith(t, &grants{answers: []answer{every(model.GrantAdded)}}, options{roll: rand.New(rand.NewPCG(1, 2))})
			ayla := owner("Ayla")
			for range boxes {
				s.g.Box(boar, killX, 0, killZ, ayla)
			}
			for _, p := range s.g.Views(ayla.ID) {
				out = append(out, *p.Items[0].Stats)
			}
		})
		return out
	}
	first, second := rolled(t), rolled(t)
	if len(first) != boxes || !slices.Equal(first, second) {
		t.Fatalf("rolled %v, then %v from the same seed", first, second)
	}
	sword, _ := worldtest.LoadData(t).Item("iron_sword")
	for _, s := range first {
		if !s.Fits(sword.Rolls) {
			t.Fatalf("rolled %+v, outside the sword's ranges", s)
		}
	}
}
