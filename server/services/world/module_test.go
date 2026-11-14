package world_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/gamedata"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/pkg/observability/metrics"
	"finga/pkg/panics"
	"finga/services/world"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	game "finga/services/world/internal/application/world"
	"finga/services/world/internal/model"
)

const tick = 10 * time.Millisecond

// characters is the character module's Load; another account's character is not found.
type characters struct {
	mu    sync.Mutex
	chars map[kernel.CharacterID]model.Character
}

func (c *characters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.chars[id]
	if !ok || ch.Account != account {
		return model.Character{}, fmt.Errorf("%w: no such character on the account", kernel.ErrCharacterNotFound)
	}
	return ch, nil
}

func (c *characters) add(ch model.Character) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chars[ch.ID] = ch
}

type saver struct{}

func (saver) SaveCheckpoint(context.Context, model.Checkpoint) error { return nil }
func (saver) Retryable(error) bool                                   { return false }
func (saver) Refused(error) bool                                     { return false }

// idle stands in for every world port these tests leave alone.
type idle struct{}

func (idle) Party(kernel.CharacterID) model.Party { return model.Party{} }
func (idle) Epoch() uint64                        { return 0 }
func (idle) Update(model.MemberUpdate)            {}
func (idle) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID) error {
	return nil
}
func (idle) AddMany(context.Context, model.GrantCall) (model.Granted, error) {
	return model.Granted{}, nil
}
func (idle) Retryable(error) bool                             { return false }
func (idle) ObserveBatch(int)                                 {}
func (idle) ObserveTick(string, time.Duration, time.Duration) {}
func (idle) CallQueued()                                      {}
func (idle) CallDone()                                        {}
func (idle) SaveDropped(string)                               {}
func (idle) Float64() float64                                 { return 0.5 }

// capturing hands the test each joined sink, to send events the world rarely sends.
type capturing struct {
	*game.World
	sinks chan instance.Sink
}

func (c capturing) Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error) {
	select {
	case c.sinks <- sink:
	default:
	}
	return c.World.Join(ctx, account, id, conn, sink)
}

type stack struct {
	world   *world.Module
	ctx     context.Context
	chars   *characters
	sinks   chan instance.Sink
	account kernel.AccountID
	data    *gamedata.Data
}

// wears answers every equip with a cap in the hat slot for 30 max HP, or with err; it reads nothing worn.
type wears struct{ err error }

func (w wears) Equip(context.Context, model.EquipCall) (model.Equipped, error) {
	item := &model.SlotItem{ID: kernel.NewItemID(), Template: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{PhysicalDefense: 3, MaxHP: 30}}
	return model.Equipped{Slots: []model.Slot{{Tab: gamedata.Equip, Slot: 0}}, Equipment: []model.EquipSlot{{Slot: "hat", Item: item}}, Worn: model.Worn{Revision: 2, Items: []model.WornItem{{Template: item.Template, Stats: *item.Stats}}}}, w.err
}

func (w wears) Unequip(context.Context, model.UnequipCall) (model.Equipped, error) {
	return model.Equipped{Equipment: []model.EquipSlot{{Slot: "hat"}}, Worn: model.Worn{Revision: 3}}, w.err
}

func (wears) Worn(context.Context, kernel.CharacterID) (model.Worn, error) { return model.Worn{}, nil }

// HandIn takes the gel and gives nothing back, at revision 4; err refuses it.
func (w wears) HandIn(context.Context, model.HandInCall) (model.Inventory, error) {
	return model.Inventory{Revision: 4, Slots: []model.Slot{{Tab: gamedata.Etc, Slot: 0}}}, w.err
}

// Use takes a red potion from the Use tab's slot 0, leaving 2, at revision 6; err refuses it.
func (w wears) Use(context.Context, model.UseCall) (model.Inventory, bool, error) {
	return model.Inventory{Revision: 6, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 0, Item: &model.SlotItem{ID: kernel.NewItemID(), Template: "red_potion", Quantity: 2}}}}, false, w.err
}

// HandedIn finds none handed in.
func (wears) HandedIn(context.Context, kernel.CharacterID, []model.Completion) ([]model.HandedIn, error) {
	return nil, nil
}

func newStack(t *testing.T) *stack { return newStackWearing(t, wears{}) }

func newStackWearing(t *testing.T, eq wears) *stack {
	t.Helper()
	data := gamedatatest.Shipped(t)
	chars := &characters{chars: map[kernel.CharacterID]model.Character{}}
	rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
	calls := itemcalls.NewPool(itemcalls.Deps{Observer: idle{}, Panics: rec}, itemcalls.Options{Workers: 1})
	w := game.New(game.Deps{
		Data: data, NewRoll: func() model.Roll { return idle{} }, Observer: idle{}, Items: idle{}, Batches: idle{}, Equipment: eq, HandIns: eq, Potions: eq, HandedIns: eq, Wardrobe: eq, ItemCalls: calls, Parties: idle{}, Traces: noop.NewTracerProvider(),
		Saver: saver{}, Characters: chars, Starter: idle{}, Panics: rec, Logger: slog.New(slog.DiscardHandler), SaveObserver: idle{},
	}, game.Tuning{TickInterval: tick, CheckpointInterval: time.Minute, FlushTimeout: time.Second, BatchMax: 32})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	var running sync.WaitGroup // the world module's three tasks
	running.Go(func() { _ = w.Run(ctx) })
	running.Go(func() { _ = w.Save(ctx) })
	running.Go(func() { _ = calls.Run(ctx) })
	t.Cleanup(func() { cancel(); running.Wait() })

	h, err := host.New(host.Options{Name: "world", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	m, err := h.Module("world")
	if err != nil {
		t.Fatal(err)
	}
	sinks := make(chan instance.Sink, 1)
	return &stack{world: world.Over(t, m, capturing{World: w, sinks: sinks}, 20), ctx: ctx, chars: chars, sinks: sinks, account: kernel.NewAccountID(), data: data}
}

func (s *stack) character(name string) model.Character {
	town := s.data.StartTown()
	ch := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: name, Class: kernel.Class("warrior"), Level: 1,
		HP: 184, Energy: 4,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ch)
	return ch
}

// gatewayStream is a gateway's subscription as the test reads it, until the stack's deadline.
type gatewayStream struct {
	sub *hub.Sub[world.Delivery]
	ctx context.Context
}

func (s *stack) gateway(id string) gatewayStream {
	return gatewayStream{sub: s.world.Subscribe(id), ctx: s.ctx}
}

func (s *stack) join(t *testing.T, gateway, conn string, ch model.Character) world.View {
	t.Helper()
	v, err := s.world.Join(s.ctx, gateway, s.account, world.Presence{Character: ch.ID, Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	if v.TickRateHz != 20 {
		t.Fatalf("tick rate %d, want the world's 20 Hz, which the client times its steps by", v.TickRateHz)
	}
	if v.JoinSeq == 0 {
		t.Fatal("the join has no sequence, which social orders logins by")
	}
	return v
}

// frame is the delivery's frame; an empty one, for no one, when it carries another event.
func frame(d world.Delivery) world.Frame {
	f, _ := d.Event.(world.Frame)
	return f
}

// recipientIn is the frame's recipient for the character; nil when the frame does not go to it.
func recipientIn(f world.Frame, id kernel.CharacterID) *world.Recipient {
	for i := range f.Recipients {
		if f.Recipients[i].Character == id {
			return &f.Recipients[i]
		}
	}
	return nil
}

func until(t *testing.T, gw gatewayStream, what string, match func(world.Delivery) bool) world.Delivery {
	t.Helper()
	for {
		select {
		case d := <-gw.sub.C():
			if match(d) {
				return d
			}
		case <-gw.sub.Done():
			t.Fatalf("waiting for %s: the stream ended", what)
		case <-gw.ctx.Done():
			t.Fatalf("waiting for %s: %v", what, gw.ctx.Err())
		}
	}
}

// next is the stream's next event of type E.
func next[E world.Event](t *testing.T, gw gatewayStream, what string) E {
	t.Helper()
	return until(t, gw, what, func(d world.Delivery) bool {
		_, ok := d.Event.(E)
		return ok
	}).Event.(E)
}

func playerIn(ps []world.PlayerView, id kernel.CharacterID) (world.PlayerView, bool) {
	for _, p := range ps {
		if p.ID == id {
			return p, true
		}
	}
	return world.PlayerView{}, false
}

func TestJoinSendsTheSnapshotThenFramesOnTheGatewaysStream(t *testing.T) {
	s := newStack(t)
	gw := s.gateway("gw-1")
	ayla := s.character("Ayla")
	view := s.join(t, "gw-1", "conn-1", ayla)
	if view.ID != ayla.ID || view.Name != "Ayla" || view.MapID != "town" || view.MaxHP != 144 || view.Stats.STR != 7 {
		t.Fatalf("view %+v", view)
	}
	first := until(t, gw, "the first delivery", func(world.Delivery) bool { return true })
	snap, ok := first.Event.(world.Snapshot)
	if !ok || first.Character != ayla.ID || first.Conn != "conn-1" || snap.MapID != "town" {
		t.Fatalf("first delivery %+v", first)
	}
	if _, ok := playerIn(snap.Players, ayla.ID); !ok {
		t.Fatalf("snapshot %+v", snap)
	}

	presence := world.Presence{Character: ayla.ID, Conn: "conn-1"}
	if err := s.world.Input(s.ctx, presence, 1, sim.Input{Right: true}, 0); err != nil {
		t.Fatal(err)
	}
	var last uint64
	for range 3 {
		d := until(t, gw, "a frame", func(d world.Delivery) bool {
			_, ok := d.Event.(world.Frame)
			return ok
		})
		f := frame(d)
		r := recipientIn(f, ayla.ID)
		if r == nil || r.Connection != "conn-1" || d.Tick <= last {
			t.Fatalf("frame %+v after tick %d", d, last)
		}
		last = d.Tick
		if p, ok := playerIn(f.Players, ayla.ID); ok && r.AckSeq == 1 && p.State.X <= ayla.X {
			t.Fatalf("held right but at %v from %v", p.State.X, ayla.X)
		}
	}
}

// The join's answer carries when each cooldown and buff the character joined with ends, for the client to show the time left.
func TestJoinAnswersWithTheSkillTimersLeft(t *testing.T) {
	s := newStack(t)
	s.gateway("gw-1")
	now := time.Now()
	town := s.data.StartTown()
	ch := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 10, HP: 184, Energy: 4,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, Skills: model.Skills{Points: map[string]int{"iron_body": 2}},
		Timers: model.SkillTimers{
			Cooldowns: map[string]time.Time{"iron_body": now.Add(40 * time.Second)},
			Buffs:     map[string]model.BuffTimer{"iron_body": {Until: now.Add(20 * time.Second), Points: 2}},
		},
	}
	s.chars.add(ch)
	timers := s.join(t, "gw-1", "conn-1", ch).Timers
	cooldown, buff := timers.Cooldowns["iron_body"], timers.Buffs["iron_body"]
	if len(timers.Cooldowns) != 1 || !cooldown.Equal(now.Add(40*time.Second)) || len(timers.Buffs) != 1 || !buff.Until.Equal(now.Add(20*time.Second)) || buff.Points != 2 {
		t.Fatalf("timers %+v, want Iron Body's cooldown and its buff at 2 points", timers)
	}
}

func TestACommandCountsOnlyFromTheConnectionThatJoined(t *testing.T) {
	s := newStack(t)
	gw := s.gateway("gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	next[world.Snapshot](t, gw, "the snapshot")

	// The world ignores an older connection's seq 1, so the joined one's input with the same seq applies.
	if err := s.world.Input(s.ctx, world.Presence{Character: ayla.ID, Conn: "conn-old"}, 1, sim.Input{Right: true}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.world.Input(s.ctx, world.Presence{Character: ayla.ID, Conn: "conn-1"}, 1, sim.Input{Left: true}, 0); err != nil {
		t.Fatal(err)
	}
	d := until(t, gw, "her move", func(d world.Delivery) bool {
		p, ok := playerIn(frame(d).Players, ayla.ID)
		return ok && p.State.X != ayla.X
	})
	if p, _ := playerIn(frame(d).Players, ayla.ID); p.State.X >= ayla.X {
		t.Fatalf("moved right to %v from %v: the older connection's input counted", p.State.X, ayla.X)
	}

	t.Run("a character that has not joined is ignored", func(t *testing.T) {
		bo := s.character("Bo")
		if err := s.world.Attack(s.ctx, world.Presence{Character: bo.ID, Conn: "conn-1"}, 0); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLeaveRemovesOnlyThroughTheConnectionThatJoined(t *testing.T) {
	s := newStack(t)
	gw := s.gateway("gw-1")
	ayla, bo := s.character("Ayla"), s.character("Bo")
	s.join(t, "gw-1", "conn-a", ayla)
	s.join(t, "gw-1", "conn-b", bo)
	until(t, gw, "Bo's snapshot", func(d world.Delivery) bool {
		_, ok := d.Event.(world.Snapshot)
		return ok && d.Character == bo.ID
	})
	if err := s.world.Leave(s.ctx, world.Presence{Character: ayla.ID, Conn: "conn-old"}); err != nil {
		t.Fatal(err)
	}
	if err := s.world.Leave(s.ctx, world.Presence{Character: ayla.ID, Conn: "conn-a"}); err != nil {
		t.Fatal(err)
	}
	// The first Leave changes nothing, the second removes her, and Bo sees her go once.
	d := until(t, gw, "her removal", func(d world.Delivery) bool {
		f := frame(d)
		return recipientIn(f, bo.ID) != nil && len(f.Removed) > 0
	})
	if got := frame(d).Removed; len(got) != 1 || got[0] != ayla.ID {
		t.Fatalf("removed %v", got)
	}
}

func TestJoinFailures(t *testing.T) {
	s := newStack(t)
	ayla := s.character("Ayla")
	t.Run("no stream for the gateway", func(t *testing.T) {
		_, err := s.world.Join(s.ctx, "gw-none", s.account, world.Presence{Character: ayla.ID, Conn: "c"})
		if !errors.Is(err, kernel.ErrNotSubscribed) {
			t.Fatalf("err = %v", err)
		}
	})
	s.gateway("gw-1")
	t.Run("someone else's character fails as the character module said", func(t *testing.T) {
		_, err := s.world.Join(s.ctx, "gw-1", kernel.NewAccountID(), world.Presence{Character: ayla.ID, Conn: "c"})
		if !errors.Is(err, kernel.ErrCharacterNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a character on a map the world does not run", func(t *testing.T) {
		lost := s.character("Lost")
		lost.MapID = "limbo"
		s.chars.add(lost)
		_, err := s.world.Join(s.ctx, "gw-1", s.account, world.Presence{Character: lost.ID, Conn: "c"})
		if !errors.Is(err, kernel.ErrNoMap) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAFullQueueEndsTheGatewaysStream(t *testing.T) {
	s := newStack(t)
	gw := s.gateway("gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	sink := <-s.sinks
	// The gateway reads nothing, so the sink eventually refuses and drops the player.
	refused := false
	for range 2_000_000 {
		if !sink.Send(model.Chat{Sender: ayla.ID, SenderName: "Ayla", Text: "spam"}) {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("the sink never refused")
	}
	select {
	case <-gw.sub.Done(): // the refusal ended it
	default:
		t.Fatal("the stream is still open")
	}
	if sink.Send(model.Chat{Sender: ayla.ID, Text: "after"}) {
		t.Fatal("a sink on an ended stream still accepts")
	}
	t.Run("a new subscription takes joins again", func(t *testing.T) {
		gw := s.gateway("gw-1")
		s.join(t, "gw-1", "conn-2", ayla)
		next[world.Snapshot](t, gw, "the snapshot")
	})
}

// Detached reaches the gateway on the connection's stream, so it joins the player again.
func TestDetachedCrossesToTheConnection(t *testing.T) {
	s := newStack(t)
	gw := s.gateway("gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	sink := <-s.sinks
	next[world.Snapshot](t, gw, "the snapshot")
	sink.Send(model.Detached{})
	d := until(t, gw, "Detached", func(d world.Delivery) bool {
		_, ok := d.Event.(world.Detached)
		return ok
	})
	if d.Conn != "conn-1" || d.Character != ayla.ID {
		t.Fatalf("got %+v", d)
	}
}

func TestEquipAnswersOnceTheStatsApply(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	ayla := s.character("Ayla")
	view := s.join(t, "gw-1", "c1", ayla)
	if view.Sheet.Derived.MaxHP != view.MaxHP || view.Sheet.Primary.STR != 7 {
		t.Fatalf("the joined view's sheet %+v", view.Sheet)
	}
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	if err := s.world.Equip(s.ctx, presence, "k-1", kernel.NewItemID(), ""); err != nil {
		t.Fatal(err)
	}
	inv := next[world.InventoryChanged](t, stream, "the equip's InventoryChanged")
	if eq := inv.Equipment; inv.Revision != 2 || len(eq) != 1 || eq[0].Slot != "hat" || eq[0].Item == nil || eq[0].Item.Stats == nil || eq[0].Item.Stats.PhysicalDefense != 3 {
		t.Fatalf("inventory change %+v", inv)
	}
	stats := next[world.StatsChanged](t, stream, "StatsChanged")
	if stats.MaxHP != 174 || stats.Sheet == nil || stats.Sheet.DerivedWorn.MaxHP != 30 || stats.Sheet.DerivedWorn.PhysicalDefense != 3 {
		t.Fatalf("stats change %+v", stats)
	}
	to := 5
	if err := s.world.Unequip(s.ctx, presence, "k-2", "hat", &to); err != nil {
		t.Fatal(err)
	}
}

// Points are spent and reset over the real world, each refusal with its kernel error; a retried Apply finds the count changed.
func TestSpendAndResetPointsAnswerOnceTheStatsApply(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 3, Points: model.Points{Unspent: 10},
		HP: 220, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	spend := func(expected int, add gamedata.Stats) error {
		return s.world.SpendPoints(s.ctx, presence, expected, add)
	}
	if err := spend(10, gamedata.Stats{STR: 4, VIT: 2}); err != nil {
		t.Fatal(err)
	}
	stats := next[world.StatsChanged](t, stream, "the spend's StatsChanged")
	if stats.Sheet == nil || stats.Sheet.Primary.STR != 11 || stats.Sheet.Primary.VIT != 9 || stats.MaxHP != 208 {
		t.Fatalf("stats change %+v", stats)
	}
	for _, c := range []struct {
		name     string
		expected int
		add      gamedata.Stats
		want     error
	}{
		{"a retried Apply", 10, gamedata.Stats{STR: 4, VIT: 2}, kernel.ErrPointsChanged},
		{"more than unspent", 4, gamedata.Stats{STR: 5}, kernel.ErrNotEnoughPoints},
		{"a negative stat", 4, gamedata.Stats{STR: -1, AGI: 2}, kernel.ErrBadPoints},
	} {
		if err := spend(c.expected, c.add); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if err := s.world.ResetPoints(s.ctx, presence, 4); err != nil {
		t.Fatal(err)
	}
	stats = next[world.StatsChanged](t, stream, "the reset's StatsChanged")
	if stats.Sheet == nil || stats.Sheet.Primary.STR != 7 || stats.Sheet.Primary.VIT != 7 || stats.MaxHP != 180 {
		t.Fatalf("stats change after the reset %+v", stats)
	}
	if err := s.world.ResetPoints(s.ctx, presence, 10); !errors.Is(err, kernel.ErrNoFreeReset) {
		t.Fatalf("a second reset: %v", err)
	}
}

func TestSpendSkillPointsAnswersOnceTheSheetApplies(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3, Skills: model.Skills{Unspent: 10},
		HP: 120, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	spend := func(expected int, add map[string]int) error {
		return s.world.SpendSkillPoints(s.ctx, presence, expected, add)
	}
	if err := spend(10, map[string]int{"second_wind": 4, "spark": 1}); err != nil {
		t.Fatal(err)
	}
	stats := next[world.StatsChanged](t, stream, "the spend's StatsChanged")
	if sh := stats.Sheet; sh == nil || sh.Skills.Unspent != 5 || sh.Skills.Points["second_wind"] != 4 || sh.Skills.Points["spark"] != 1 || math.Abs(sh.DerivedSkills.HPRegen-0.9) > 1e-9 {
		t.Fatalf("stats change %+v; want 5 left, the points invested, and Second Wind's 0.9 HP a second", stats)
	}
	for _, c := range []struct {
		name     string
		expected int
		add      map[string]int
		want     error
	}{
		{"a retried Apply", 10, map[string]int{"second_wind": 4}, kernel.ErrSkillPointsChanged},
		{"more than unspent", 5, map[string]int{"strike": 6}, kernel.ErrNotEnoughSkillPoints},
		{"a skill no class teaches", 5, map[string]int{"fireball": 1}, kernel.ErrSkillNotLearnable},
		{"a negative count", 5, map[string]int{"strike": -1}, kernel.ErrBadSkillPoints},
	} {
		if err := spend(c.expected, c.add); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// UseSkill answers once the tick resolves it, with SkillUsed on the stream, or with the refusal's kernel error.
func TestUseSkillAnswersOnceTheTickResolvesIt(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	var hunting *gamedata.Map
	for _, m := range s.data.Maps() {
		if m.Kind == gamedata.Hunting && hunting == nil {
			hunting = m
		}
	}
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3,
		Skills: model.Skills{Points: map[string]int{"strike": 1, "spark": 1, "second_wind": 1}},
		HP:     120, Energy: 2, MapID: hunting.ID, X: hunting.PlayerSpawn.X, Y: hunting.PlayerSpawn.Y,
	}
	town := s.data.StartTown()
	bo := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Bo", Class: kernel.Class("beginner"), Level: 3,
		Skills: model.Skills{Points: map[string]int{"strike": 1}}, HP: 120, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.chars.add(bo)
	s.join(t, "gw-1", "c1", ayla)
	s.join(t, "gw-1", "c2", bo)
	use := func(ch model.Character, conn, skill string) error {
		return s.world.UseSkill(s.ctx, world.Presence{Character: ch.ID, Conn: conn}, skill, 0)
	}
	if err := use(ayla, "c1", "strike"); err != nil {
		t.Fatal(err)
	}
	used := next[world.SkillUsed](t, stream, "Strike's SkillUsed")
	if used.Caster != ayla.ID || used.Skill != "strike" || used.Stage != 0 || used.Facing != 1 {
		t.Fatalf("skill used %+v", used)
	}
	for _, c := range []struct {
		name  string
		who   model.Character
		conn  string
		skill string
		want  error
	}{
		{"during its cooldown", ayla, "c1", "strike", kernel.ErrSkillCoolingDown},
		{"with no points in it", ayla, "c1", "throw", kernel.ErrSkillNotLearned},
		{"a passive", ayla, "c1", "second_wind", kernel.ErrSkillPassive},
		{"short of energy, Strike having spent it", ayla, "c1", "spark", kernel.ErrNotEnoughEnergy},
		{"damaging, in a town", bo, "c2", "strike", kernel.ErrNotInTown},
	} {
		if err := use(c.who, c.conn, c.skill); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// A trainer's dialog carries its promotion, and Promote answers each refusal with its kernel error.
func TestPromoteAnswersWithItsRefusals(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3,
		HP: 120, MapID: town.ID, X: 1120, Y: 608, // beside the Warrior Trainer
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	if err := s.world.Talk(s.ctx, presence, "warrior_trainer"); err != nil {
		t.Fatal(err)
	}
	offer := next[world.NpcDialog](t, stream, "the trainer's dialog").Promotion
	if offer == nil || offer.Class != "warrior" || len(offer.Conditions) != 4 || offer.Conditions[0].Kind != world.ConditionLevel || offer.Conditions[0].Need != 10 || offer.Conditions[0].Have != 3 {
		t.Fatalf("offer %+v, want the Warrior's four conditions, level 3 of 10 first", offer)
	}
	for _, c := range []struct {
		name  string
		npc   string
		class kernel.Class
		want  error
	}{
		{"short of its conditions", "warrior_trainer", "warrior", kernel.ErrPromotionConditionsUnmet},
		{"a class the trainer doesn't train", "warrior_trainer", "thief", kernel.ErrNotYourNextClass},
		{"from a trainer out of reach", "magician_trainer", "magician", kernel.ErrNpcOutOfReach},
	} {
		if err := s.world.Promote(s.ctx, presence, c.npc, c.class); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func TestAnEquipRefusalKeepsTheItemModulesReason(t *testing.T) {
	s := newStackWearing(t, wears{err: fmt.Errorf("%w: the cap asks a higher level", kernel.ErrLevelTooLow)})
	s.gateway("gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "c1", ayla)
	err := s.world.Equip(s.ctx, world.Presence{Character: ayla.ID, Conn: "c1"}, "k-1", kernel.NewItemID(), "")
	if !errors.Is(err, kernel.ErrLevelTooLow) {
		t.Fatalf("got %v, want the item module's level_too_low", err)
	}
}

// A potion answers once its HP applies; refusals keep their kernel errors, the world's and the item module's.
func TestUseItemAnswersOnceTheHPApplies(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	ayla := s.character("Ayla")
	ayla.HP = 50
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	use := func(template string) error {
		return s.world.UseItem(s.ctx, presence, "k-1", kernel.NewItemID(), template)
	}
	if err := use("red_potion"); err != nil {
		t.Fatal(err)
	}
	inv := next[world.InventoryChanged](t, stream, "the use's InventoryChanged")
	if inv.Revision != 6 || len(inv.Slots) != 1 || inv.Slots[0].Item == nil || inv.Slots[0].Item.Quantity != 2 {
		t.Fatalf("inventory change %+v", inv)
	}
	if stats := next[world.StatsChanged](t, stream, "StatsChanged"); stats.HP != 100 {
		t.Fatalf("stats change %+v, want HP 50 + 50", stats)
	}
	// The world's refusal and the item module's share kernel.ErrNotUsable; only the world's has its model error under it.
	if err := use("slime_gel"); !errors.Is(err, kernel.ErrNotUsable) || !errors.Is(err, model.ErrNotUsable) {
		t.Fatalf("a material: %v, want the world's not_usable", err)
	}
}

func TestAUseRefusalKeepsTheItemModulesReason(t *testing.T) {
	s := newStackWearing(t, wears{err: fmt.Errorf("%w: the stack holds another template", kernel.ErrNotUsable)})
	s.gateway("gw-1")
	ayla := s.character("Ayla")
	ayla.HP = 50
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	err := s.world.UseItem(s.ctx, world.Presence{Character: ayla.ID, Conn: "c1"}, "k-1", kernel.NewItemID(), "red_potion")
	if !errors.Is(err, kernel.ErrNotUsable) || errors.Is(err, model.ErrNotUsable) {
		t.Fatalf("got %v, want the item module's not_usable", err)
	}
}

// Talk answers once the dialog is on the stream, computed from the quest log the join loaded.
func TestTalkSendsTheDialogComputedFromTheLoadedLog(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestFinished, TimesFinished: 1}},
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	if err := s.world.Talk(s.ctx, presence, "town_guide"); err != nil {
		t.Fatal(err)
	}
	got := next[world.NpcDialog](t, stream, "the dialog")
	want := world.NpcDialog{NPC: "town_guide", Gives: []world.QuestOffer{
		{Quest: "slime_cleanup", State: world.OfferFinished},
		{Quest: "sticky_samples", State: world.OfferAvailable},
		{Quest: "meet_the_trainers", State: world.OfferAvailable},
		{Quest: "ready_for_the_wilds", State: world.OfferAvailable},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dialog %+v, want %+v", got, want)
	}
	if err := s.world.Talk(s.ctx, presence, "nobody"); !errors.Is(err, kernel.ErrNpcOutOfReach) {
		t.Fatalf("an NPC not on the map: %v", err)
	}
}

// The join answers with the quest log; the quest calls answer once applied, each change on the stream, and keep the item module's reason for a refused hand-in.
func TestQuestsOverTheWorld(t *testing.T) {
	s := newStack(t)
	stream := s.gateway("gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestFinished, TimesFinished: 1}},
	}
	s.chars.add(ayla)
	log := s.join(t, "gw-1", "c1", ayla).Quests
	if q, ok := log["slime_cleanup"]; len(log) != 1 || !ok || q.Status != world.QuestFinished || len(q.Progress) != 0 || q.TimesFinished != 1 {
		t.Fatalf("joined with the log %+v", log)
	}
	presence := world.Presence{Character: ayla.ID, Conn: "c1"}
	changed := func(what string) world.QuestChanged { return next[world.QuestChanged](t, stream, what) }
	if err := s.world.AcceptQuest(s.ctx, presence, "town_guide", "sticky_samples"); err != nil {
		t.Fatal(err)
	}
	if c := changed("the accept"); c.Change != world.QuestAccepted || c.ID != "sticky_samples" {
		t.Fatalf("change %+v", c)
	}
	if err := s.world.AcceptQuest(s.ctx, presence, "town_guide", "sticky_samples"); !errors.Is(err, kernel.ErrQuestAlreadyActive) {
		t.Fatalf("a second accept: %v", err)
	}
	if err := s.world.HandInQuest(s.ctx, presence, "town_guide", "sticky_samples"); err != nil {
		t.Fatal(err)
	}
	if c := changed("the hand-in"); c.Change != world.QuestHandedIn || c.Quest.Status != world.QuestFinished {
		t.Fatalf("change %+v", c)
	}
	if err := s.world.AbandonQuest(s.ctx, presence, "sticky_samples"); err != nil {
		t.Fatalf("abandoning a quest not active: %v", err)
	}
}

func TestARefusedHandInKeepsTheItemModulesReason(t *testing.T) {
	s := newStackWearing(t, wears{err: fmt.Errorf("%w: no gel in the Etc tab", kernel.ErrNotEnoughItems)})
	s.gateway("gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"sticky_samples": {Status: model.QuestActive, Progress: []int{0}}},
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	err := s.world.HandInQuest(s.ctx, world.Presence{Character: ayla.ID, Conn: "c1"}, "town_guide", "sticky_samples")
	if !errors.Is(err, kernel.ErrNotEnoughItems) {
		t.Fatalf("got %v", err)
	}
}
