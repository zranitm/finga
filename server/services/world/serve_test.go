package world_test

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/observability/metrics"
	"finga/pkg/panics"
	"finga/services/world"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	game "finga/services/world/internal/application/world"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/model"
)

const tick = 10 * time.Millisecond

// characters is the character service's Load; another account's character is not found.
type characters struct {
	mu    sync.Mutex
	chars map[kernel.CharacterID]model.Character
}

func (c *characters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.chars[id]
	if !ok || ch.Account != account {
		return model.Character{}, grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
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

// idle stands in for every world service these tests leave alone.
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
	c       worldv1.WorldServiceClient
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
	srv := handler.New(world.Over(t, m, capturing{World: w, sinks: sinks}), handler.Options{BootID: "boot-1", TickRateHz: 20})
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	worldv1.RegisterWorldServiceServer(g, srv)
	return &stack{c: worldv1.NewWorldServiceClient(grpctest.Conn(t, g)), ctx: ctx, chars: chars, sinks: sinks, account: kernel.NewAccountID(), data: data}
}

func (s *stack) character(name string) model.Character {
	town := s.data.StartTown()
	ch := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: name, Class: kernel.Class("warrior"), Level: 1,
		HP: 184, MP: 28,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ch)
	return ch
}

func (s *stack) gateway(t *testing.T, id string) grpc.ServerStreamingClient[worldv1.DeliverResponse] {
	t.Helper()
	stream, err := s.c.Deliver(s.ctx, &worldv1.DeliverRequest{GatewayId: id})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %+v %v", m, err)
	}
	return stream
}

func (s *stack) join(t *testing.T, gateway, conn string, ch model.Character) *worldv1.CharacterView {
	t.Helper()
	res, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: gateway, ConnectionId: conn, AccountId: s.account.String(), CharacterId: ch.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetTickRateHz() != 20 {
		t.Fatalf("tick rate %d, want the world's 20 Hz, which the client times its steps by", res.GetTickRateHz())
	}
	if res.GetJoinSeq() == 0 {
		t.Fatal("the join has no sequence, which social orders logins by")
	}
	return res.GetCharacter()
}

// recipientIn is the frame's recipient for the character; nil when the frame does not go to it.
func recipientIn(f *worldv1.Frame, id string) *worldv1.Recipient {
	for _, r := range f.GetRecipients() {
		if r.GetCharacterId() == id {
			return r
		}
	}
	return nil
}

func until(t *testing.T, stream grpc.ServerStreamingClient[worldv1.DeliverResponse], what string, match func(*worldv1.DeliverResponse) bool) *worldv1.DeliverResponse {
	t.Helper()
	for {
		m, err := stream.Recv()
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if match(m) {
			return m
		}
	}
}

func playerIn(ps []*worldv1.PlayerView, id string) (*worldv1.PlayerView, bool) {
	for _, p := range ps {
		if p.GetId() == id {
			return p, true
		}
	}
	return nil, false
}

func TestJoinSendsTheSnapshotThenFramesOnTheGatewaysStream(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	view := s.join(t, "gw-1", "conn-1", ayla)
	if view.GetId() != ayla.ID.String() || view.GetName() != "Ayla" || view.GetMapId() != "town" || view.GetMaxHp() != 144 || view.GetStats().GetStrength() != 7 {
		t.Fatalf("view %+v", view)
	}
	first, err := gw.Recv()
	if err != nil {
		t.Fatal(err)
	}
	snap := first.GetSnapshot()
	if first.GetCharacterId() != ayla.ID.String() || first.GetConnectionId() != "conn-1" || snap.GetMapId() != "town" {
		t.Fatalf("first delivery %+v", first)
	}
	if _, ok := playerIn(snap.GetPlayers(), ayla.ID.String()); !ok {
		t.Fatalf("snapshot %+v", snap)
	}

	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "conn-1"}
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: presence, Seq: 1, Keys: &worldv1.Keys{Right: true}}); err != nil {
		t.Fatal(err)
	}
	var last uint64
	for range 3 {
		m := until(t, gw, "a frame", func(m *worldv1.DeliverResponse) bool { return m.GetFrame() != nil })
		r := recipientIn(m.GetFrame(), ayla.ID.String())
		if r == nil || r.GetConnectionId() != "conn-1" || m.GetTick() <= last {
			t.Fatalf("frame %+v after tick %d", m, last)
		}
		last = m.GetTick()
		if p, ok := playerIn(m.GetFrame().GetDelta().GetPlayers(), ayla.ID.String()); ok && r.GetAckSeq() == 1 && p.GetX() <= ayla.X {
			t.Fatalf("held right but at %v from %v", p.GetX(), ayla.X)
		}
	}
}

// The join's answer carries the time left on each cooldown and buff the character joined with, for the client to show.
func TestJoinAnswersWithTheSkillTimersLeft(t *testing.T) {
	s := newStack(t)
	s.gateway(t, "gw-1")
	now := time.Now()
	town := s.data.StartTown()
	ch := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 10, HP: 184, MP: 28,
		MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y, Skills: model.Skills{Points: map[string]int{"iron_body": 2}},
		Timers: model.SkillTimers{
			Cooldowns: map[string]time.Time{"iron_body": now.Add(40 * time.Second)},
			Buffs:     map[string]model.BuffTimer{"iron_body": {Until: now.Add(20 * time.Second), Points: 2}},
		},
	}
	s.chars.add(ch)
	res, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "conn-1", AccountId: s.account.String(), CharacterId: ch.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	timers := res.GetTimers()
	if len(timers) != 2 || timers[0].GetSkillId() != "iron_body" || timers[0].GetKind() != worldv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN || timers[0].GetLeftMs() <= 30_000 || timers[0].GetLeftMs() > 40_000 ||
		timers[1].GetKind() != worldv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF || timers[1].GetLeftMs() <= 10_000 || timers[1].GetLeftMs() > 20_000 || timers[1].GetPoints() != 2 {
		t.Fatalf("timers %v, want Iron Body's cooldown then its buff at 2 points", timers)
	}
}

func TestACommandCountsOnlyFromTheConnectionThatJoined(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })

	// The world ignores an older connection's seq 1, so the joined one's input with the same seq applies.
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "conn-old"}, Seq: 1, Keys: &worldv1.Keys{Right: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "conn-1"}, Seq: 1, Keys: &worldv1.Keys{Left: true}}); err != nil {
		t.Fatal(err)
	}
	m := until(t, gw, "her move", func(m *worldv1.DeliverResponse) bool {
		p, ok := playerIn(m.GetFrame().GetDelta().GetPlayers(), ayla.ID.String())
		return ok && p.GetX() != ayla.X
	})
	if p, _ := playerIn(m.GetFrame().GetDelta().GetPlayers(), ayla.ID.String()); p.GetX() >= ayla.X {
		t.Fatalf("moved right to %v from %v: the older connection's input counted", p.GetX(), ayla.X)
	}

	t.Run("a character that has not joined is ignored", func(t *testing.T) {
		bo := s.character("Bo")
		if _, err := s.c.Attack(s.ctx, &worldv1.AttackRequest{Presence: &worldv1.Presence{CharacterId: bo.ID.String(), ConnectionId: "conn-1"}}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a bad character ID", func(t *testing.T) {
		_, err := s.c.Attack(s.ctx, &worldv1.AttackRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "conn-1"}})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_BAD_ID) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a pickup, retired, is answered and does nothing", func(t *testing.T) {
		if _, err := s.c.PickUp(s.ctx, &worldv1.PickUpRequest{Presence: &worldv1.Presence{CharacterId: "nope"}}); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no connection", func(t *testing.T) {
		_, err := s.c.EnterPortal(s.ctx, &worldv1.EnterPortalRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String()}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLeaveRemovesOnlyThroughTheConnectionThatJoined(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
	ayla, bo := s.character("Ayla"), s.character("Bo")
	s.join(t, "gw-1", "conn-a", ayla)
	s.join(t, "gw-1", "conn-b", bo)
	until(t, gw, "Bo's snapshot", func(m *worldv1.DeliverResponse) bool {
		return m.GetCharacterId() == bo.ID.String() && m.GetSnapshot() != nil
	})
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "conn-old"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "conn-a"}}); err != nil {
		t.Fatal(err)
	}
	// The first Leave changes nothing, the second removes her, and Bo sees her go once.
	m := until(t, gw, "her removal", func(m *worldv1.DeliverResponse) bool {
		return recipientIn(m.GetFrame(), bo.ID.String()) != nil && len(m.GetFrame().GetDelta().GetRemovedPlayers()) > 0
	})
	if got := m.GetFrame().GetDelta().GetRemovedPlayers(); len(got) != 1 || got[0] != ayla.ID.String() {
		t.Fatalf("removed %v", got)
	}
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("leave without a connection: %v", err)
	}
}

func TestJoinFailures(t *testing.T) {
	s := newStack(t)
	ayla := s.character("Ayla")
	t.Run("no stream for the gateway", func(t *testing.T) {
		_, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-none", ConnectionId: "c", AccountId: s.account.String(), CharacterId: ayla.ID.String()})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED) {
			t.Fatalf("err = %v", err)
		}
	})
	s.gateway(t, "gw-1")
	t.Run("someone else's character fails as the character service said", func(t *testing.T) {
		_, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "c", AccountId: kernel.NewAccountID().String(), CharacterId: ayla.ID.String()})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(characterv1.Reason_REASON_CHARACTER_NOT_FOUND) || status.Code(err) != codes.NotFound {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a character on a map the world does not run", func(t *testing.T) {
		lost := s.character("Lost")
		lost.MapID = "limbo"
		s.chars.add(lost)
		_, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "c", AccountId: s.account.String(), CharacterId: lost.ID.String()})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_NO_MAP) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAFullQueueEndsTheGatewaysStream(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
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
	for {
		_, err := gw.Recv()
		if err == nil {
			continue
		}
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("stream ended with %v", err)
		}
		break
	}
	if sink.Send(model.Chat{Sender: ayla.ID, Text: "after"}) {
		t.Fatal("a sink on an ended stream still accepts")
	}
	t.Run("a new subscription takes joins again", func(t *testing.T) {
		gw := s.gateway(t, "gw-1")
		s.join(t, "gw-1", "conn-2", ayla)
		until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })
	})
}

// Detached reaches the gateway on the connection's stream, so it joins the player again.
func TestDetachedCrossesToTheConnection(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	sink := <-s.sinks
	until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })
	sink.Send(model.Detached{})
	m := until(t, gw, "Detached", func(m *worldv1.DeliverResponse) bool { return m.GetDetached() != nil })
	if m.GetConnectionId() != "conn-1" || m.GetCharacterId() != ayla.ID.String() {
		t.Fatalf("got %+v", m)
	}
}

func TestEquipAnswersOnceTheStatsApply(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	view := s.join(t, "gw-1", "c1", ayla)
	if view.GetSheet().GetDerived().GetMaxHp() != view.GetMaxHp() || view.GetSheet().GetPrimary().GetStrength() != 7 {
		t.Fatalf("the joined view's sheet %+v", view.GetSheet())
	}
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	if _, err := s.c.Equip(s.ctx, &worldv1.EquipRequest{Presence: presence, IdempotencyKey: "k-1", ItemId: kernel.NewItemID().String()}); err != nil {
		t.Fatal(err)
	}
	inv := until(t, stream, "the equip's InventoryChanged", func(m *worldv1.DeliverResponse) bool { return m.GetInventoryChanged() != nil }).GetInventoryChanged()
	if eq := inv.GetEquipment(); inv.GetRevision() != 2 || len(eq) != 1 || eq[0].GetEquipSlot() != "hat" || eq[0].GetItem().GetStats().GetPhysicalDefense() != 3 {
		t.Fatalf("inventory change %+v", inv)
	}
	stats := until(t, stream, "StatsChanged", func(m *worldv1.DeliverResponse) bool { return m.GetStatsChanged() != nil }).GetStatsChanged()
	if stats.GetMaxHp() != 174 || stats.GetSheet().GetDerivedWorn().GetMaxHp() != 30 || stats.GetSheet().GetDerivedWorn().GetPhysicalDefense() != 3 {
		t.Fatalf("stats change %+v", stats)
	}
	to := int32(5)
	if _, err := s.c.Unequip(s.ctx, &worldv1.UnequipRequest{Presence: presence, IdempotencyKey: "k-2", EquipSlot: "hat", ToSlot: &to}); err != nil {
		t.Fatal(err)
	}
}

// Points are spent and reset over the real world, each refusal with its reason; a retried Apply finds the count changed.
func TestSpendAndResetPointsAnswerOnceTheStatsApply(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 3, Points: model.Points{Unspent: 10},
		HP: 220, MP: 40, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	spend := func(expected int32, add *worldv1.Stats) error {
		_, err := s.c.SpendPoints(s.ctx, &worldv1.SpendPointsRequest{Presence: presence, ExpectedUnspent: expected, Points: add})
		return err
	}
	if err := spend(10, &worldv1.Stats{Strength: 4, Vitality: 2}); err != nil {
		t.Fatal(err)
	}
	stats := until(t, stream, "the spend's StatsChanged", func(m *worldv1.DeliverResponse) bool { return m.GetStatsChanged() != nil }).GetStatsChanged()
	if p := stats.GetSheet().GetPrimary(); p.GetStrength() != 11 || p.GetVitality() != 9 || stats.GetMaxHp() != 208 {
		t.Fatalf("stats change %+v", stats)
	}
	for _, c := range []struct {
		name     string
		expected int32
		add      *worldv1.Stats
		code     codes.Code
		reason   worldv1.Reason
	}{
		{"a retried Apply", 10, &worldv1.Stats{Strength: 4, Vitality: 2}, codes.FailedPrecondition, worldv1.Reason_REASON_POINTS_CHANGED},
		{"more than unspent", 4, &worldv1.Stats{Strength: 5}, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_ENOUGH_POINTS},
		{"a negative stat", 4, &worldv1.Stats{Strength: -1, Agility: 2}, codes.InvalidArgument, worldv1.Reason_REASON_BAD_POINTS},
	} {
		err := spend(c.expected, c.add)
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(c.reason) || status.Code(err) != c.code {
			t.Fatalf("%s: %v, want %v %v", c.name, err, c.code, c.reason)
		}
	}
	if _, err := s.c.ResetPoints(s.ctx, &worldv1.ResetPointsRequest{Presence: presence, ExpectedUnspent: 4}); err != nil {
		t.Fatal(err)
	}
	stats = until(t, stream, "the reset's StatsChanged", func(m *worldv1.DeliverResponse) bool { return m.GetStatsChanged() != nil }).GetStatsChanged()
	if p := stats.GetSheet().GetPrimary(); p.GetStrength() != 7 || p.GetVitality() != 7 || stats.GetMaxHp() != 180 {
		t.Fatalf("stats change after the reset %+v", stats)
	}
	_, err := s.c.ResetPoints(s.ctx, &worldv1.ResetPointsRequest{Presence: presence, ExpectedUnspent: 10})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(worldv1.Reason_REASON_NO_FREE_RESET) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second reset: %v", err)
	}
	if _, err := s.c.SpendPoints(s.ctx, &worldv1.SpendPointsRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
	if _, err := s.c.ResetPoints(s.ctx, &worldv1.ResetPointsRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no connection: %v", err)
	}
}

func TestSpendSkillPointsAnswersOnceTheSheetApplies(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3, Skills: model.Skills{Unspent: 10},
		HP: 120, MP: 40, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	spend := func(expected int32, add map[string]int32) error {
		_, err := s.c.SpendSkillPoints(s.ctx, &worldv1.SpendSkillPointsRequest{Presence: presence, ExpectedUnspent: expected, Points: add})
		return err
	}
	if err := spend(10, map[string]int32{"second_wind": 4, "spark": 1}); err != nil {
		t.Fatal(err)
	}
	stats := until(t, stream, "the spend's StatsChanged", func(m *worldv1.DeliverResponse) bool { return m.GetStatsChanged() != nil }).GetStatsChanged()
	if sk := stats.GetSheet().GetSkills(); sk.GetUnspent() != 5 || sk.GetInvested()["second_wind"] != 4 || sk.GetInvested()["spark"] != 1 || math.Abs(stats.GetSheet().GetDerivedSkills().GetHpRegen()-0.9) > 1e-9 {
		t.Fatalf("stats change %+v; want 5 left, the points invested, and Second Wind's 0.9 HP a second", stats)
	}
	for _, c := range []struct {
		name     string
		expected int32
		add      map[string]int32
		code     codes.Code
		reason   worldv1.Reason
	}{
		{"a retried Apply", 10, map[string]int32{"second_wind": 4}, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_POINTS_CHANGED},
		{"more than unspent", 5, map[string]int32{"strike": 6}, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS},
		{"a skill no class teaches", 5, map[string]int32{"fireball": 1}, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_NOT_LEARNABLE},
		{"a negative count", 5, map[string]int32{"strike": -1}, codes.InvalidArgument, worldv1.Reason_REASON_BAD_SKILL_POINTS},
	} {
		err := spend(c.expected, c.add)
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(c.reason) || status.Code(err) != c.code {
			t.Fatalf("%s: %v, want %v %v", c.name, err, c.code, c.reason)
		}
	}
	if _, err := s.c.SpendSkillPoints(s.ctx, &worldv1.SpendSkillPointsRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
}

// UseSkill answers once the tick resolves it, with SkillUsed on the stream, or with the refusal's reason.
func TestUseSkillAnswersOnceTheTickResolvesIt(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	var hunting *gamedata.Map
	for _, m := range s.data.Maps() {
		if m.Kind == gamedata.Hunting && hunting == nil {
			hunting = m
		}
	}
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3,
		Skills: model.Skills{Points: map[string]int{"strike": 1, "spark": 1, "second_wind": 1}},
		HP:     120, MP: 4, MapID: hunting.ID, X: hunting.PlayerSpawn.X, Y: hunting.PlayerSpawn.Y,
	}
	town := s.data.StartTown()
	bo := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Bo", Class: kernel.Class("beginner"), Level: 3,
		Skills: model.Skills{Points: map[string]int{"strike": 1}}, HP: 120, MP: 40, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
	}
	s.chars.add(ayla)
	s.chars.add(bo)
	s.join(t, "gw-1", "c1", ayla)
	s.join(t, "gw-1", "c2", bo)
	use := func(ch model.Character, conn, skill string) error {
		_, err := s.c.UseSkill(s.ctx, &worldv1.UseSkillRequest{Presence: &worldv1.Presence{CharacterId: ch.ID.String(), ConnectionId: conn}, Skill: skill})
		return err
	}
	if err := use(ayla, "c1", "strike"); err != nil {
		t.Fatal(err)
	}
	used := until(t, stream, "Strike's SkillUsed", func(m *worldv1.DeliverResponse) bool { return m.GetSkillUsed() != nil }).GetSkillUsed()
	if used.GetCaster() != ayla.ID.String() || used.GetSkill() != "strike" || used.GetStage() != 0 || used.GetFacing() != 1 {
		t.Fatalf("skill used %+v", used)
	}
	for _, c := range []struct {
		name   string
		who    model.Character
		conn   string
		skill  string
		reason worldv1.Reason
	}{
		{"during its cooldown", ayla, "c1", "strike", worldv1.Reason_REASON_SKILL_COOLING_DOWN},
		{"with no points in it", ayla, "c1", "throw", worldv1.Reason_REASON_SKILL_NOT_LEARNED},
		{"a passive", ayla, "c1", "second_wind", worldv1.Reason_REASON_SKILL_PASSIVE},
		{"short of MP", ayla, "c1", "spark", worldv1.Reason_REASON_NOT_ENOUGH_MP},
		{"damaging, in a town", bo, "c2", "strike", worldv1.Reason_REASON_NOT_IN_TOWN},
	} {
		err := use(c.who, c.conn, c.skill)
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(c.reason) || status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s: %v, want %v", c.name, err, c.reason)
		}
	}
	if _, err := s.c.UseSkill(s.ctx, &worldv1.UseSkillRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}, Skill: "strike"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
}

// A trainer's dialog carries its promotion, and Promote answers each refusal with its reason.
func TestPromoteAnswersWithItsRefusals(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	town := s.data.StartTown()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("beginner"), Level: 3,
		HP: 120, MP: 40, MapID: town.ID, X: 1120, Y: 608, // beside the Warrior Trainer
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	if _, err := s.c.Talk(s.ctx, &worldv1.TalkRequest{Presence: presence, NpcId: "warrior_trainer"}); err != nil {
		t.Fatal(err)
	}
	offer := until(t, stream, "the trainer's dialog", func(m *worldv1.DeliverResponse) bool { return m.GetNpcDialog() != nil }).GetNpcDialog().GetPromotion()
	level := offer.GetConditions()[0]
	if offer.GetClass() != "warrior" || len(offer.GetConditions()) != 4 || level.GetKind() != worldv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL || level.GetNeed() != 10 || level.GetHave() != 3 {
		t.Fatalf("offer %+v, want the Warrior's four conditions, level 3 of 10 first", offer)
	}
	for _, c := range []struct {
		name   string
		npc    string
		class  string
		reason worldv1.Reason
	}{
		{"short of its conditions", "warrior_trainer", "warrior", worldv1.Reason_REASON_PROMOTION_CONDITIONS_UNMET},
		{"a class the trainer doesn't train", "warrior_trainer", "thief", worldv1.Reason_REASON_NOT_YOUR_NEXT_CLASS},
		{"from a trainer out of reach", "magician_trainer", "magician", worldv1.Reason_REASON_NPC_OUT_OF_REACH},
	} {
		_, err := s.c.Promote(s.ctx, &worldv1.PromoteRequest{Presence: presence, NpcId: c.npc, Class: c.class})
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(c.reason) || status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s: %v, want %v", c.name, err, c.reason)
		}
	}
}

func TestAnEquipRefusalKeepsTheItemServicesReason(t *testing.T) {
	refused := grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(itemv1.Reason_REASON_LEVEL_TOO_LOW))
	s := newStackWearing(t, wears{err: refused})
	s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "c1", ayla)
	_, err := s.c.Equip(s.ctx, &worldv1.EquipRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}, IdempotencyKey: "k-1", ItemId: kernel.NewItemID().String()})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(itemv1.Reason_REASON_LEVEL_TOO_LOW) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v, want the item service's level_too_low", err)
	}
	if _, err := s.c.Equip(s.ctx, &worldv1.EquipRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}, IdempotencyKey: "k-1", ItemId: "nope"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed item ID: %v", err)
	}
}

// A potion answers once its HP applies; refusals keep their reasons, the world's and the item service's.
func TestUseItemAnswersOnceTheHPApplies(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	ayla.HP, ayla.MP = 50, 1_000_000 // a join holds MP to the maximum
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	use := func(template, item string) error {
		_, err := s.c.UseItem(s.ctx, &worldv1.UseItemRequest{Presence: presence, IdempotencyKey: "k-1", ItemId: item, Template: template})
		return err
	}
	if err := use("red_potion", kernel.NewItemID().String()); err != nil {
		t.Fatal(err)
	}
	inv := until(t, stream, "the use's InventoryChanged", func(m *worldv1.DeliverResponse) bool { return m.GetInventoryChanged() != nil }).GetInventoryChanged()
	if inv.GetRevision() != 6 || len(inv.GetSlots()) != 1 || inv.GetSlots()[0].GetItem().GetQuantity() != 2 {
		t.Fatalf("inventory change %+v", inv)
	}
	if stats := until(t, stream, "StatsChanged", func(m *worldv1.DeliverResponse) bool { return m.GetStatsChanged() != nil }).GetStatsChanged(); stats.GetHp() != 100 {
		t.Fatalf("stats change %+v, want HP 50 + 50", stats)
	}
	for _, c := range []struct {
		name, template string
		reason         worldv1.Reason
	}{
		{"an MP potion at full MP", "blue_potion", worldv1.Reason_REASON_MP_FULL},
		{"a material", "slime_gel", worldv1.Reason_REASON_NOT_USABLE},
	} {
		err := use(c.template, kernel.NewItemID().String())
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(c.reason) || status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s: %v, want %v", c.name, err, c.reason)
		}
	}
	if err := use("red_potion", "nope"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed item ID: %v", err)
	}
}

func TestAUseRefusalKeepsTheItemServicesReason(t *testing.T) {
	refused := grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(itemv1.Reason_REASON_NOT_USABLE))
	s := newStackWearing(t, wears{err: refused})
	s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	ayla.HP = 50
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	_, err := s.c.UseItem(s.ctx, &worldv1.UseItemRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}, IdempotencyKey: "k-1", ItemId: kernel.NewItemID().String(), Template: "red_potion"})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(itemv1.Reason_REASON_NOT_USABLE) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v, want the item service's not_usable", err)
	}
}

// Talk answers once the dialog is on the stream, computed from the quest log the join loaded.
func TestTalkSendsTheDialogComputedFromTheLoadedLog(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MP: 40, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestFinished, TimesFinished: 1}},
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	if _, err := s.c.Talk(s.ctx, &worldv1.TalkRequest{Presence: presence, NpcId: "town_guide"}); err != nil {
		t.Fatal(err)
	}
	got := until(t, stream, "the dialog", func(m *worldv1.DeliverResponse) bool { return m.GetNpcDialog() != nil }).GetNpcDialog()
	want := &worldv1.NpcDialog{NpcId: "town_guide", Gives: []*worldv1.QuestOffer{
		{QuestId: "slime_cleanup", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
		{QuestId: "sticky_samples", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
		{QuestId: "meet_the_trainers", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
		{QuestId: "ready_for_the_wilds", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
	}}
	if !proto.Equal(got, want) {
		t.Fatalf("dialog %v, want %v", got, want)
	}
	_, err := s.c.Talk(s.ctx, &worldv1.TalkRequest{Presence: presence, NpcId: "nobody"})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(worldv1.Reason_REASON_NPC_OUT_OF_REACH) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("an NPC not on the map: %v", err)
	}
	if _, err := s.c.Talk(s.ctx, &worldv1.TalkRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
}

// The join answers with the quest log; the quest RPCs answer once applied, each change on the stream, and keep the item service's reason for a refused hand-in.
func TestQuestsOverTheWorld(t *testing.T) {
	s := newStack(t)
	stream := s.gateway(t, "gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MP: 40, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestFinished, TimesFinished: 1}},
	}
	s.chars.add(ayla)
	res, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "c1", AccountId: s.account.String(), CharacterId: ayla.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if log := res.GetQuests(); len(log) != 1 || !proto.Equal(log[0], &worldv1.QuestState{QuestId: "slime_cleanup", Status: worldv1.QuestStatus_QUEST_STATUS_FINISHED, Progress: []int32{}, TimesFinished: 1}) {
		t.Fatalf("joined with the log %v", log)
	}
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}
	changed := func(what string) *worldv1.QuestChanged {
		return until(t, stream, what, func(m *worldv1.DeliverResponse) bool { return m.GetQuestChanged() != nil }).GetQuestChanged()
	}
	if _, err := s.c.AcceptQuest(s.ctx, &worldv1.AcceptQuestRequest{Presence: presence, NpcId: "town_guide", QuestId: "sticky_samples"}); err != nil {
		t.Fatal(err)
	}
	if c := changed("the accept"); c.GetChange() != worldv1.QuestChange_QUEST_CHANGE_ACCEPTED || c.GetQuest().GetQuestId() != "sticky_samples" {
		t.Fatalf("change %v", c)
	}
	_, err = s.c.AcceptQuest(s.ctx, &worldv1.AcceptQuestRequest{Presence: presence, NpcId: "town_guide", QuestId: "sticky_samples"})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second accept: %v", err)
	}
	if _, err := s.c.HandInQuest(s.ctx, &worldv1.HandInQuestRequest{Presence: presence, NpcId: "town_guide", QuestId: "sticky_samples"}); err != nil {
		t.Fatal(err)
	}
	if c := changed("the hand-in"); c.GetChange() != worldv1.QuestChange_QUEST_CHANGE_HANDED_IN || c.GetQuest().GetStatus() != worldv1.QuestStatus_QUEST_STATUS_FINISHED {
		t.Fatalf("change %v", c)
	}
	if _, err := s.c.AbandonQuest(s.ctx, &worldv1.AbandonQuestRequest{Presence: presence, QuestId: "sticky_samples"}); err != nil {
		t.Fatalf("abandoning a quest not active: %v", err)
	}
	if _, err := s.c.AcceptQuest(s.ctx, &worldv1.AcceptQuestRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
	if _, err := s.c.AbandonQuest(s.ctx, &worldv1.AbandonQuestRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("no connection: %v", err)
	}
	if _, err := s.c.HandInQuest(s.ctx, &worldv1.HandInQuestRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "c1"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a malformed character ID: %v", err)
	}
}

func TestARefusedHandInKeepsTheItemServicesReason(t *testing.T) {
	s := newStackWearing(t, wears{err: grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(itemv1.Reason_REASON_NOT_ENOUGH_ITEMS))})
	s.gateway(t, "gw-1")
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 2, HP: 100, MP: 40, MapID: "town", X: 450, Y: 608,
		Quests: model.Quests{"sticky_samples": {Status: model.QuestActive, Progress: []int{0}}},
	}
	s.chars.add(ayla)
	s.join(t, "gw-1", "c1", ayla)
	_, err := s.c.HandInQuest(s.ctx, &worldv1.HandInQuestRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), ConnectionId: "c1"}, NpcId: "town_guide", QuestId: "sticky_samples"})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(itemv1.Reason_REASON_NOT_ENOUGH_ITEMS) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
}
