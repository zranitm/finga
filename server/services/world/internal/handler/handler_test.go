package handler_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/panics"
	"finga/pkg/sentineltest"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/world"
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

func (idle) Members(kernel.CharacterID) []kernel.CharacterID  { return nil }
func (idle) SameParty(_, _ kernel.CharacterID) bool           { return false }
func (idle) Update(kernel.CharacterID, string, int, int, int) {}
func (idle) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID, kernel.Class) error {
	return nil
}
func (idle) Add(context.Context, model.Pickup) ([]model.Slot, error) { return nil, nil }
func (idle) Retryable(error) bool                                    { return false }
func (idle) Granted(error) bool                                      { return false }
func (idle) Inventory(context.Context, kernel.CharacterID) ([]model.Slot, error) {
	return nil, nil
}
func (idle) ObserveTick(string, time.Duration, time.Duration) {}
func (idle) JobStarted()                                      {}
func (idle) JobFinished()                                     {}
func (idle) SaveDropped(string)                               {}
func (idle) Float64() float64                                 { return 0.5 }

// capturing hands the test each joined sink, to send events the world rarely sends.
type capturing struct {
	*world.World
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

func newStack(t *testing.T) *stack {
	t.Helper()
	data, err := gamedata.Load(filepath.Join("..", "..", "..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	chars := &characters{chars: map[kernel.CharacterID]model.Character{}}
	rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
	w := world.New(world.Deps{
		Data: data, NewRoll: func() model.Roll { return idle{} }, Observer: idle{}, Items: idle{}, Workers: economy.NewWorkers(economy.Deps{Observer: idle{}, Panics: rec}, economy.Options{Workers: 1}), Parties: idle{}, Traces: noop.NewTracerProvider(),
		Saver: saver{}, Characters: chars, Starter: idle{}, Panics: rec, Logger: slog.New(slog.DiscardHandler), SaveObserver: idle{},
	}, world.Tuning{TickInterval: tick, CheckpointInterval: time.Minute, FlushTimeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	sinks := make(chan instance.Sink, 1)
	srv := handler.New(capturing{World: w, sinks: sinks}, handler.Options{BootID: "boot-1", TickRateHz: 20, Logger: slog.New(slog.DiscardHandler)})
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	worldv1.RegisterWorldServiceServer(g, srv)
	return &stack{c: worldv1.NewWorldServiceClient(grpctest.Conn(t, g)), ctx: ctx, chars: chars, sinks: sinks, account: kernel.NewAccountID(), data: data}
}

func (s *stack) character(name string) model.Character {
	town := s.data.Town()
	ch := model.Character{
		ID: kernel.NewCharacterID(), Account: s.account, Name: name, Class: kernel.Warrior, Level: 1,
		Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MP: 28,
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
	if view.GetId() != ayla.ID.String() || view.GetName() != "Ayla" || view.GetMapId() != "town" || view.GetMaxHp() != 204 || view.GetStats().GetStrength() != 12 {
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
		_, err := s.c.PickUp(s.ctx, &worldv1.PickUpRequest{Presence: &worldv1.Presence{CharacterId: "nope", ConnectionId: "conn-1"}})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_BAD_ID) {
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

func TestPickupFailuresCrossWithTheirReason(t *testing.T) {
	s := newStack(t)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	sink := <-s.sinks
	until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })
	cases := []struct {
		name string
		err  error
		want grpcx.Reason
	}{
		{"the item service's, passed through", grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL)},
		{"the world's own", model.ErrPickupBusy, grpcx.Key(worldv1.Reason_REASON_PICKUP_BUSY)},
		{"one nobody expected crosses empty", errors.New("disk on fire"), grpcx.Reason{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sink.Send(model.PickupFailed{Tick: 7, Err: c.err})
			m := until(t, gw, "the failure", func(m *worldv1.DeliverResponse) bool { return m.GetPickupFailed() != nil })
			f := m.GetPickupFailed()
			if m.GetTick() != 7 || f.GetErrorDomain() != c.want.Domain || f.GetErrorReason() != c.want.Name {
				t.Fatalf("got %+v", m)
			}
		})
	}
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

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentineltest.Declared(filepath.Join("..", "model"))
	if err != nil || len(declared) < 2 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentineltest.Referenced("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in Errors", name)
		}
	}
	used := map[grpcx.Reason]bool{}
	for _, row := range handler.Errors {
		used[grpcx.Key(row.Reason)] = true
	}
	for _, r := range grpctest.Reasons(worldv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
