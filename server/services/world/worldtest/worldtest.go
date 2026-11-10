// Package worldtest is the world's test support: game data, recording sinks, and maps and Worlds built from Options.
package worldtest

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/panics"
	"finga/pkg/tracing"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/model"
)

// TickInterval is the tick Options give a map by default.
const TickInterval = 50 * time.Millisecond

// Delta is a tick's frame as one player got it.
type Delta struct {
	*model.Frame
	AckSeq uint32
}

// Sink records a player's events, and each frame as its Delta: the player has a stream of its own.
type Sink struct {
	mu     sync.Mutex
	events []any // model.Event, or Delta
	full   bool  // sends are refused: a slow consumer
}

// Send records e.
func (s *Sink) Send(e model.Event) bool { return s.record(e) }

// Stream is the sink itself.
func (s *Sink) Stream() instance.Stream { return s }

// SendFrame records the frame as the player's Delta.
func (s *Sink) SendFrame(f *model.Frame, to []model.Recipient) bool {
	return s.record(Delta{Frame: f, AckSeq: to[0].AckSeq})
}

func (s *Sink) record(e any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.full {
		return false
	}
	s.events = append(s.events, e)
	return true
}

// Refusing is a sink that refuses every send.
func Refusing() *Sink { return &Sink{full: true} }

// Refuse makes every later send fail, as a slow consumer's.
func (s *Sink) Refuse() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.full = true
}

// All is everything recorded, in order.
func (s *Sink) All() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

// Deltas are the frames recorded.
func (s *Sink) Deltas() []Delta {
	return Of[Delta](s)
}

// Last is the latest Delta, or an empty one.
func (s *Sink) Last() Delta {
	ds := s.Deltas()
	if len(ds) == 0 {
		return Delta{Frame: &model.Frame{}}
	}
	return ds[len(ds)-1]
}

// Own finds the player's own view in the latest Delta that carries it.
func (s *Sink) Own(id kernel.CharacterID) (model.PlayerView, bool) {
	for _, d := range slices.Backward(s.Deltas()) {
		for _, p := range d.Players {
			if p.ID == id {
				return p, true
			}
		}
	}
	return model.PlayerView{}, false
}

// Of is what s recorded of type T.
func Of[T any](s *Sink) []T {
	var out []T
	for _, e := range s.All() {
		if v, ok := e.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// Observer counts the ticks it sees.
type Observer struct {
	mu    sync.Mutex
	ticks []time.Duration
}

// ObserveTick records how long a tick took.
func (o *Observer) ObserveTick(_ string, took, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ticks = append(o.ticks, took)
}

// Ticks is how many ticks it saw.
func (o *Observer) Ticks() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ticks)
}

// LoadData loads the game data the server ships.
func LoadData(t *testing.T) *gamedata.Data {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	d, err := gamedata.Load(filepath.Join(filepath.Dir(here), "..", "..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TownSpawn is where players appear in the town.
func TownSpawn(t *testing.T, data *gamedata.Data) (float64, float64) {
	t.Helper()
	town := data.Town()
	return town.PlayerSpawn.X, town.PlayerSpawn.Y
}

// NewPlayer is a level 1 warrior named name at (x, y).
func NewPlayer(name string, x, y float64) model.Player {
	return model.Player{ID: kernel.NewCharacterID(), Name: name, Class: kernel.Warrior, Level: 1, Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MP: 28, X: x, Y: y}
}

// FixedRoll rolls v every time.
type FixedRoll struct{ V float64 }

// Float64 is v.
func (r FixedRoll) Float64() float64 { return r.V }

// Rolls gives each map a FixedRoll of v.
func Rolls(v float64) func() model.Roll {
	return func() model.Roll { return FixedRoll{v} }
}

// DiscardPanics records panics to nowhere.
func DiscardPanics() *panics.Recorder { return panics.NewRecorder(slog.New(slog.DiscardHandler), nil) }

// Spans records every span Traces samples.
var Spans = tracetest.NewSpanRecorder()

// Traces samples as the world does.
var Traces = worldTraces()

func worldTraces() *tracing.Provider {
	tp, err := tracing.New(context.Background(), tracing.Config{}, tracing.Options{Service: "world", Root: loot.EntryPoint})
	if err != nil {
		panic(err)
	}
	tp.RegisterSpanProcessor(Spans)
	return tp
}

// Options are what a test sets; Fill gives the rest a 50 ms tick, rolls of 0.5, grant calls of up to 32 drops and services that do nothing.
type Options struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	FlushTimeout       time.Duration
	NewRoll            func() model.Roll
	Observer           instance.TickObserver
	Transfer           instance.Mover // a map's; the World moves players itself
	Items              loot.Items
	Batches            loot.Observer
	BatchMax           int // drops per grant call
	Equipment          instance.Equipment
	ItemCalls          *itemcalls.Pool
	Parties            instance.Parties
	Characters         world.Characters
	Starter            world.Starter
	Wardrobe           world.Wardrobe
	Logger             *slog.Logger // the World's
	Traces             trace.TracerProvider
	SaveObserver       world.SaveObserver
}

// Fill gives every option left unset its default.
func (o Options) Fill() Options {
	o.TickInterval = cmp.Or(o.TickInterval, TickInterval)
	o.CheckpointInterval = cmp.Or(o.CheckpointInterval, 30*time.Second)
	o.FlushTimeout = cmp.Or(o.FlushTimeout, 10*time.Second)
	if o.NewRoll == nil {
		o.NewRoll = Rolls(0.5)
	}
	if o.Observer == nil {
		o.Observer = &Observer{}
	}
	if o.Transfer == nil {
		o.Transfer = nobody{}
	}
	if o.Items == nil {
		o.Items = nobody{}
	}
	if o.Batches == nil {
		o.Batches = nobody{}
	}
	o.BatchMax = cmp.Or(o.BatchMax, 32)
	if o.Equipment == nil {
		o.Equipment = nobody{}
	}
	if o.ItemCalls == nil {
		o.ItemCalls = itemcalls.NewPool(itemcalls.Deps{Observer: nobody{}, Panics: DiscardPanics()}, itemcalls.Options{Workers: 1})
	}
	if o.Parties == nil {
		o.Parties = nobody{}
	}
	if o.Characters == nil {
		o.Characters = nobody{}
	}
	if o.Starter == nil {
		o.Starter = nobody{}
	}
	if o.Wardrobe == nil {
		o.Wardrobe = nobody{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Traces == nil {
		o.Traces = Traces
	}
	if o.SaveObserver == nil {
		o.SaveObserver = nobody{}
	}
	return o
}

// Services are the filled options every map takes.
func (o Options) Services() instance.Services {
	return instance.Services{NewRoll: o.NewRoll, Observer: o.Observer, Items: o.Items, Batches: o.Batches, Equipment: o.Equipment, ItemCalls: o.ItemCalls, Parties: o.Parties, Traces: o.Traces}
}

// Checkpoints records what a map puts for saving.
type Checkpoints struct {
	mu  sync.Mutex
	cps []model.Checkpoint
}

// Put records cp.
func (c *Checkpoints) Put(cp model.Checkpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cps = append(c.cps, cp)
}

// Drain is what was put since the last drain.
func (c *Checkpoints) Drain() []model.Checkpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.cps
	c.cps = nil
	return out
}

// NewInstance builds m's map with o filled in; cps may be nil.
func NewInstance(m *gamedata.Map, data *gamedata.Data, cps *Checkpoints, o Options) *instance.Instance {
	o = o.Fill()
	if cps == nil {
		cps = &Checkpoints{}
	}
	return instance.New(instance.Deps{Services: o.Services(), Map: m, Data: data, Saves: cps, Mover: o.Transfer, Logger: o.Logger}, instance.Tuning{TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, BatchMax: o.BatchMax})
}

// NewWorld builds a World over data with o filled in.
func NewWorld(data *gamedata.Data, saver world.Saver, o Options) *world.World {
	o = o.Fill()
	return world.New(world.Deps{
		Services: o.Services(), Data: data, Saver: saver, Characters: o.Characters, Starter: o.Starter, Wardrobe: o.Wardrobe,
		Panics: DiscardPanics(), Logger: o.Logger, SaveObserver: o.SaveObserver,
	}, world.Tuning{TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, FlushTimeout: o.FlushTimeout, BatchMax: o.BatchMax})
}

// Run starts a town map in the synctest bubble; stop waits for Run so nothing outlives it.
func Run(t *testing.T, data *gamedata.Data, cps *Checkpoints, o Options) (*instance.Instance, func()) {
	t.Helper()
	return RunMap(t, data, data.Town(), cps, o)
}

// RunMap starts m's map in the synctest bubble, as Run does the town's.
func RunMap(t *testing.T, data *gamedata.Data, m *gamedata.Map, cps *Checkpoints, o Options) (*instance.Instance, func()) {
	t.Helper()
	inst := NewInstance(m, data, cps, o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = inst.Run(ctx) }()
	var once sync.Once
	return inst, func() { once.Do(func() { cancel(); <-done }) }
}

// Walk sends the same Input once per tick for n ticks after seq, returning the last seq used.
func Walk(inst *instance.Instance, id kernel.CharacterID, seq uint32, in sim.Input, ticks int) uint32 {
	for range ticks {
		seq++
		_ = inst.Input(id, seq, in)
		time.Sleep(TickInterval)
		synctest.Wait()
	}
	return seq
}

// errNobody is the answer of a service no test set.
var errNobody = errors.New("worldtest: no such service in this test")

// nobody is every service a test left unset: it does nothing, and refuses what needs an answer.
type nobody struct{}

// Transfer moves nobody.
func (nobody) Transfer(*instance.Instance, model.Player, instance.Sink, string) {}

// Dropped forgets nobody.
func (nobody) Dropped(*instance.Instance, kernel.CharacterID, string) {}

// Party is none.
func (nobody) Party(kernel.CharacterID) model.Party { return model.Party{} }

// Epoch never changes.
func (nobody) Epoch() uint64 { return 0 }

// Update tells nobody.
func (nobody) Update(kernel.CharacterID, string, int, int, int) {}

// CallQueued counts nothing.
func (nobody) CallQueued() {}

// CallDone counts nothing.
func (nobody) CallDone() {}

// Retryable is never.
func (nobody) Retryable(error) bool { return false }

// SaveDropped counts nothing.
func (nobody) SaveDropped(string) {}

// AddMany refuses.
func (nobody) AddMany(context.Context, model.GrantCall) (model.Granted, error) {
	return model.Granted{}, errNobody
}

// ObserveBatch counts nothing.
func (nobody) ObserveBatch(int) {}

// GrantStarter grants nothing, and does not fail a join.
func (nobody) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID, kernel.Class) error {
	return nil
}

// Load finds no character.
func (nobody) Load(context.Context, kernel.CharacterID, kernel.AccountID) (model.Character, error) {
	return model.Character{}, errNobody
}

// Equip refuses.
func (nobody) Equip(context.Context, model.EquipCall) (model.Equipped, error) {
	return model.Equipped{}, errNobody
}

// Unequip refuses.
func (nobody) Unequip(context.Context, model.UnequipCall) (model.Equipped, error) {
	return model.Equipped{}, errNobody
}

// Worn wears nothing, so a join goes on.
func (nobody) Worn(context.Context, kernel.CharacterID) (model.Worn, error) { return model.Worn{}, nil }
