// Package worldtest is the world's test support: game data, recording sinks, and maps and Worlds built from Options.
package worldtest

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/itemcalls"
	"finga/internal/application/world/loot"
	"finga/internal/application/world/world"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/pkg/observability/tracing"
	"finga/pkg/panics"
	"finga/pkg/runner"
)

// TickInterval is the tick Options give a map by default.
const TickInterval = 50 * time.Millisecond

// Delta is a tick's frame as one player got it: their parts as one, as the client reads them, and what they stopped seeing.
type Delta struct {
	*model.Part
	Tick    uint64
	AckSeq  uint32
	Energy  *int          // set when the player's energy changed, and in the first frame after a join
	Unseen  model.Unseen  // what they stopped seeing
	Pouches model.Pouches // the player's own that changed
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
	r := to[0]
	d := Delta{Part: Joined(f, r.Parts), Tick: f.Tick, AckSeq: r.AckSeq, Energy: r.Energy, Pouches: r.Pouches}
	if r.Unseen != nil {
		d.Unseen = *r.Unseen
	}
	return s.record(d)
}

// Joined is a frame's parts as one, each list theirs in turn, as protobuf merges them.
func Joined(f *model.Frame, parts []int) *model.Part {
	out := &model.Part{}
	for _, k := range parts {
		p := f.Parts[k]
		out.Players = append(out.Players, p.Players...)
		out.Mobs = append(out.Mobs, p.Mobs...)
		out.Removed = append(out.Removed, p.Removed...)
		out.RemovedMobs = append(out.RemovedMobs, p.RemovedMobs...)
		if p.Boss != nil {
			out.Boss = p.Boss
		}
	}
	return out
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
		return Delta{Part: &model.Part{}}
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

// Observer counts the ticks it sees, and the channel of the last.
type Observer struct {
	mu      sync.Mutex
	ticks   []time.Duration
	channel int
}

// ObserveTick records how long a tick took.
func (o *Observer) ObserveTick(_ string, channel int, took, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ticks = append(o.ticks, took)
	o.channel = channel
}

// Channel is the region's channel the last tick it saw ran, 0 off regions.
func (o *Observer) Channel() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.channel
}

// Ticks is how many ticks it saw.
func (o *Observer) Ticks() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.ticks)
}

// edited is what LoadDataWith loaded, by its edited files' names and texts, so tests making the same edits load them once.
var edited sync.Map

// LoadData is the game data the server ships, shared by every test: it is read-only, as LoadDataWith's is.
func LoadData(t testing.TB) *gamedata.Data {
	t.Helper()
	return gamedatatest.Shipped(t)
}

// LoadDataWith is the game data the server ships, each file named in edits changed by its function first, a new one from empty, or left out where it's nil; tests making the same edits share it.
func LoadDataWith(t testing.TB, edits map[string]func(string) string) *gamedata.Data {
	t.Helper()
	src := gamedatatest.Dir()
	texts := map[string]string{}
	var key strings.Builder
	for _, name := range slices.Sorted(maps.Keys(edits)) {
		if edits[name] == nil {
			key.WriteString(name + "\x01")
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		texts[name] = edits[name](string(raw))
		key.WriteString(name + "\x00" + texts[name] + "\x00")
	}
	if d, ok := edited.Load(key.String()); ok {
		return d.(*gamedata.Data)
	}
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	for name, edit := range edits {
		if edit == nil {
			if err := os.Remove(filepath.Join(dst, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, text := range texts {
		if err := os.WriteFile(filepath.Join(dst, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := load.Dir(dst)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := edited.LoadOrStore(key.String(), d)
	return shared.(*gamedata.Data)
}

// WithoutRegions is edits leaving out every region the data ships too, for an interest block one would fit in, which the loader refuses.
func WithoutRegions(t testing.TB, edits map[string]func(string) string) map[string]func(string) string {
	t.Helper()
	plans, err := filepath.Glob(filepath.Join(gamedatatest.Dir(), "regions", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		edits[filepath.Join("regions", filepath.Base(p))] = nil
	}
	return edits
}

// TownSpawn is where players appear in the town, x and z.
func TownSpawn(t *testing.T, data *gamedata.Data) (float64, float64) {
	t.Helper()
	town := data.StartTown()
	return town.PlayerSpawn.X, town.PlayerSpawn.Z
}

// Flat is flat ground size metres square.
func Flat(size float64) sim.Geometry {
	return sim.Geometry{SizeX: size, SizeZ: size, Terrain: sim.NewTerrain(size, 2, 2, []float64{0, 0, 0, 0}, 0.7), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}
}

// Gridded is m with the walk grid of its geometry, as the loader gives every map, built now.
func Gridded(t testing.TB, m *gamedata.Map) *gamedata.Map {
	t.Helper()
	grid := nav.New(m.Geometry, LoadData(t).Movement().Step)
	m.Grid = func() *nav.Grid { return grid }
	return m
}

// NewPlayer is a level 1 warrior named name at (x, z), which a join stands on the ground there.
func NewPlayer(name string, x, z float64) model.Player {
	return model.Player{ID: kernel.NewCharacterID(), Name: name, Class: "warrior", Level: 1, HP: 204, X: x, Z: z}
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

// Options are what a test sets; Fill gives the rest a 50 ms tick, rolls of 0.5, grant calls of up to 32 drops, region channels of 150 closing after 5 idle minutes, and services that do nothing.
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
	ChannelCap         int // a region's channel's players
	ChannelIdle        time.Duration
	Channel            int // the region's channel a map run alone is
	Equipment          instance.Equipment
	HandIns            instance.HandIns
	Gifts              instance.Gifts
	Feats              instance.Feats
	Looks              instance.Looks
	Potions            instance.Potions
	ItemCalls          *itemcalls.Pool
	Parties            instance.Parties
	Characters         world.Characters
	Starter            world.Starter
	Wardrobe           world.Wardrobe
	HandedIns          world.HandedIns
	Logger             *slog.Logger // the World's
	Traces             trace.TracerProvider
	SaveObserver       world.SaveObserver
	Channels           world.ChannelObserver
	Results            instance.Results // matches' outcomes
}

// Fill gives every option left unset its default.
func (o Options) Fill() Options {
	o.TickInterval = cmp.Or(o.TickInterval, TickInterval)
	o.CheckpointInterval = cmp.Or(o.CheckpointInterval, 30*time.Second)
	o.FlushTimeout = cmp.Or(o.FlushTimeout, 10*time.Second)
	o.ChannelCap = cmp.Or(o.ChannelCap, 150)
	o.ChannelIdle = cmp.Or(o.ChannelIdle, 5*time.Minute)
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
	if o.HandIns == nil {
		o.HandIns = nobody{}
	}
	if o.Gifts == nil {
		o.Gifts = nobody{}
	}
	if o.Feats == nil {
		o.Feats = nobody{}
	}
	if o.Looks == nil {
		o.Looks = nobody{}
	}
	if o.Potions == nil {
		o.Potions = nobody{}
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
	if o.HandedIns == nil {
		o.HandedIns = nobody{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Traces == nil {
		o.Traces = Traces
	}
	if o.Channels == nil {
		o.Channels = nobody{}
	}
	if o.SaveObserver == nil {
		o.SaveObserver = nobody{}
	}
	if o.Results == nil {
		o.Results = &Results{}
	}
	return o
}

// Services are the filled options every map takes.
func (o Options) Services() instance.Services {
	return instance.Services{NewRoll: o.NewRoll, Observer: o.Observer, Items: o.Items, Batches: o.Batches, Equipment: o.Equipment, HandIns: o.HandIns, Gifts: o.Gifts, Feats: o.Feats, Looks: o.Looks, Potions: o.Potions, ItemCalls: o.ItemCalls, Parties: o.Parties, Traces: o.Traces}
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
	return instance.New(instance.Deps{Services: o.Services(), Map: m, Data: data, Saves: cps, Mover: o.Transfer, Logger: o.Logger, Channel: o.Channel}, instance.Tuning{TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, BatchMax: o.BatchMax})
}

// World is a World built for a test, with the item calls pool its maps submit to.
type World struct {
	*world.World
	calls *itemcalls.Pool
}

// Run runs the world module's three vital tasks under a runner until ctx ends, as the process does.
func (w *World) Run(ctx context.Context) error {
	return runner.New(runner.Options{ShutdownGrace: time.Second}, nil, []runner.Task{
		{Name: "world", Kind: runner.Vital, Run: w.World.Run},
		{Name: "saves", Kind: runner.Vital, Run: w.Save},
		{Name: "item calls", Kind: runner.Vital, Run: w.calls.Run},
	}).Run(ctx)
}

// NewWorld builds a World over data with o filled in.
func NewWorld(data *gamedata.Data, saver world.Saver, o Options) *World {
	o = o.Fill()
	return &World{World: world.New(world.Deps{
		Services: o.Services(), Data: data, Saver: saver, Characters: o.Characters, Starter: o.Starter, Wardrobe: o.Wardrobe, HandedIns: o.HandedIns,
		Panics: DiscardPanics(), Logger: o.Logger, SaveObserver: o.SaveObserver, Channels: o.Channels, Results: o.Results,
	}, world.Tuning{
		TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, FlushTimeout: o.FlushTimeout, BatchMax: o.BatchMax,
		ChannelCap: o.ChannelCap, ChannelIdle: o.ChannelIdle,
	}), calls: o.ItemCalls}
}

// Run starts a town map in the synctest bubble; stop waits for Run so nothing outlives it.
func Run(t *testing.T, data *gamedata.Data, cps *Checkpoints, o Options) (*instance.Instance, func()) {
	t.Helper()
	return RunMap(t, data, data.StartTown(), cps, o)
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

// RunMatch starts spec's match on its mode's map in the synctest bubble, as RunMap does a map's; its outcome goes to o.Results.
func RunMatch(t *testing.T, data *gamedata.Data, spec model.MatchSpec, cps *Checkpoints, o Options) (*instance.Instance, func()) {
	t.Helper()
	o = o.Fill()
	mode, ok := data.PvpMode(spec.Mode)
	if !ok {
		t.Fatalf("no mode %s", spec.Mode)
	}
	m, _ := data.Map(mode.Map)
	if cps == nil {
		cps = &Checkpoints{}
	}
	inst := instance.New(instance.Deps{Services: o.Services(), Map: m, Data: data, Saves: cps, Mover: o.Transfer, Logger: o.Logger, Match: &instance.Match{Spec: spec, Mode: mode, Results: o.Results}}, instance.Tuning{TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, BatchMax: o.BatchMax})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = inst.Run(ctx) }()
	var once sync.Once
	return inst, func() { once.Do(func() { cancel(); <-done }) }
}

// Results records matches' outcomes.
type Results struct {
	mu  sync.Mutex
	got []model.MatchResult
}

// Ended records r.
func (r *Results) Ended(res model.MatchResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, res)
}

// All is every outcome recorded, in order.
func (r *Results) All() []model.MatchResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.got)
}

// Moves records the players a map hands off, and where to, and the players it drops.
type Moves struct {
	mu    sync.Mutex
	got   []Move
	drops []kernel.CharacterID
}

// Move is one hand-off.
type Move struct {
	Player model.Player
	Dest   instance.Dest
}

// Transfer records the hand-off.
func (m *Moves) Transfer(_ *instance.Instance, p model.Player, _ instance.Sink, dest instance.Dest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.got = append(m.got, Move{Player: p, Dest: dest})
}

// Dropped records the drop.
func (m *Moves) Dropped(_ *instance.Instance, id kernel.CharacterID, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drops = append(m.drops, id)
}

// Drops is every player dropped, in order.
func (m *Moves) Drops() []kernel.CharacterID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.drops)
}

// All is every hand-off recorded, in order.
func (m *Moves) All() []Move {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.got)
}

// Walk sends the same Input once per tick for n ticks after seq, returning the last seq used.
func Walk(inst *instance.Instance, id kernel.CharacterID, seq uint32, in sim.Input, ticks int) uint32 {
	for range ticks {
		seq++
		_ = inst.Input(id, seq, in, 0)
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
func (nobody) Transfer(*instance.Instance, model.Player, instance.Sink, instance.Dest) {}

// Dropped forgets nobody.
func (nobody) Dropped(*instance.Instance, kernel.CharacterID, string) {}

// Party is none.
func (nobody) Party(kernel.CharacterID) model.Party { return model.Party{} }

// Epoch never changes.
func (nobody) Epoch() uint64 { return 0 }

// Update tells nobody.
func (nobody) Update(model.MemberUpdate) {}

// CallQueued counts nothing.
func (nobody) CallQueued() {}

// CallDone counts nothing.
func (nobody) CallDone() {}

// Retryable is never.
func (nobody) Retryable(error) bool { return false }

// SaveDropped counts nothing.
func (nobody) SaveDropped(string) {}

// ChannelsOpen hears nothing.
func (nobody) ChannelsOpen(string, int) {}

// ChannelStopped hears nothing.
func (nobody) ChannelStopped(string, int) {}

// AddMany refuses.
func (nobody) AddMany(context.Context, model.GrantCall) (model.Granted, error) {
	return model.Granted{}, errNobody
}

// ObserveBatch counts nothing.
func (nobody) ObserveBatch(int) {}

// GrantStarter grants nothing, and does not fail a join.
func (nobody) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID) error {
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

// Use refuses.
func (nobody) Use(context.Context, model.UseCall) (model.Inventory, bool, error) {
	return model.Inventory{}, false, errNobody
}

// Worn wears nothing, so a join goes on.
func (nobody) Worn(context.Context, kernel.CharacterID) (model.Worn, error) { return model.Worn{}, nil }

// HandedIn finds none handed in, so a join goes on.
func (nobody) HandedIn(context.Context, kernel.CharacterID, []model.Completion) ([]model.HandedIn, error) {
	return nil, nil
}

// HandIn refuses.
func (nobody) HandIn(context.Context, model.HandInCall) (model.HandedIn, error) {
	return model.HandedIn{}, errNobody
}

// GrantFeatPieces refuses.
func (nobody) GrantFeatPieces(context.Context, kernel.AccountID, []model.FeatPiece) ([]model.FeatPiece, error) {
	return nil, errNobody
}

// Give refuses.
func (nobody) Give(context.Context, model.GiftCall) (model.Gift, error) {
	return model.Gift{}, errNobody
}

// SetLook refuses.
func (nobody) SetLook(context.Context, model.LookCall) (model.Look, error) {
	return model.Look{}, errNobody
}

// Look is the class's own in every slot, so a join goes on.
func (nobody) Look(context.Context, kernel.CharacterID) (model.Look, error) { return model.Look{}, nil }
