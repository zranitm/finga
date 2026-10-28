package handler_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentinels"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/world"
)

const tick = 10 * time.Millisecond

// characters is the character service's Load: someone else's character
// fails with its not-found reason.
type characters struct {
	mu    sync.Mutex
	chars map[model.CharacterID]world.Character
}

func (c *characters) Load(_ context.Context, id model.CharacterID, account model.AccountID) (world.Character, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.chars[id]
	if !ok || ch.Account != account {
		return world.Character{}, grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
	}
	return ch, nil
}

func (c *characters) add(ch world.Character) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chars[ch.ID] = ch
}

type saver struct{}

func (saver) SaveCheckpoint(context.Context, world.Checkpoint) error { return nil }
func (saver) SaveLevelUp(context.Context, world.LevelUpSave) error   { return nil }
func (saver) Retryable(error) bool                                   { return false }

// capturing hands the test each sink the server joins with, so it can send
// events the world would only send in a long game.
type capturing struct {
	*world.World
	sinks chan world.Sink
}

func (c capturing) Join(ctx context.Context, account model.AccountID, id model.CharacterID, conn string, sink world.Sink) (world.View, error) {
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
	sinks   chan world.Sink
	account model.AccountID
	data    *gamedata.Data
}

func newStack(t *testing.T, queue int) *stack {
	t.Helper()
	data, err := gamedata.Load(filepath.Join("..", "..", "..", "..", "data"))
	if err != nil {
		t.Fatal(err)
	}
	chars := &characters{chars: map[model.CharacterID]world.Character{}}
	w := world.New(data, saver{}, slog.New(slog.DiscardHandler), world.Options{TickInterval: tick, Characters: chars})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	sinks := make(chan world.Sink, 1)
	srv := handler.New(capturing{World: w, sinks: sinks}, handler.Options{BootID: "boot-1", Queue: queue})
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	worldv1.RegisterWorldServiceServer(g, srv)
	return &stack{c: worldv1.NewWorldServiceClient(grpctest.Conn(t, g)), ctx: ctx, chars: chars, sinks: sinks, account: model.NewAccountID(), data: data}
}

// character is a warrior of the stack's account standing on the town's
// spawn.
func (s *stack) character(name string) world.Character {
	town := s.data.Town()
	ch := world.Character{
		ID: model.NewCharacterID(), Account: s.account, Name: name, Class: model.Warrior, Level: 1,
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

func (s *stack) join(t *testing.T, gateway, conn string, ch world.Character) *worldv1.CharacterView {
	t.Helper()
	res, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: gateway, ConnectionId: conn, AccountId: s.account.String(), CharacterId: ch.ID.String()})
	if err != nil {
		t.Fatal(err)
	}
	return res.GetCharacter()
}

// until reads the stream until match accepts a message.
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

func TestJoinSendsTheSnapshotThenDeltasOnTheGatewaysStream(t *testing.T) {
	s := newStack(t, 0)
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
	if first.GetCharacterId() != ayla.ID.String() || snap.GetMapId() != "town" {
		t.Fatalf("first delivery %+v", first)
	}
	if _, ok := playerIn(snap.GetPlayers(), ayla.ID.String()); !ok {
		t.Fatalf("snapshot %+v", snap)
	}

	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), MapId: "town"}
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: presence, Seq: 1, Keys: &worldv1.Keys{Right: true}}); err != nil {
		t.Fatal(err)
	}
	var last uint64
	for range 3 {
		m := until(t, gw, "a delta", func(m *worldv1.DeliverResponse) bool { return m.GetDelta() != nil })
		if m.GetCharacterId() != ayla.ID.String() || m.GetTick() <= last {
			t.Fatalf("delta %+v after tick %d", m, last)
		}
		last = m.GetTick()
		if p, ok := playerIn(m.GetDelta().GetPlayers(), ayla.ID.String()); ok && m.GetDelta().GetAckSeq() == 1 && p.GetX() <= ayla.X {
			t.Fatalf("held right but at %v from %v", p.GetX(), ayla.X)
		}
	}
}

func TestACommandGoesToTheMapItNames(t *testing.T) {
	s := newStack(t, 0)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })

	// The forest does not hold her: right with seq 1 is ignored there, so the
	// town's left with the same seq is the one applied.
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), MapId: "forest"}, Seq: 1, Keys: &worldv1.Keys{Right: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.c.Input(s.ctx, &worldv1.InputRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), MapId: "town"}, Seq: 1, Keys: &worldv1.Keys{Left: true}}); err != nil {
		t.Fatal(err)
	}
	m := until(t, gw, "her move", func(m *worldv1.DeliverResponse) bool {
		p, ok := playerIn(m.GetDelta().GetPlayers(), ayla.ID.String())
		return ok && p.GetX() != ayla.X
	})
	if p, _ := playerIn(m.GetDelta().GetPlayers(), ayla.ID.String()); p.GetX() >= ayla.X {
		t.Fatalf("moved right to %v from %v: the forest's input reached the town", p.GetX(), ayla.X)
	}

	t.Run("a map the world does not run", func(t *testing.T) {
		_, err := s.c.Attack(s.ctx, &worldv1.AttackRequest{Presence: &worldv1.Presence{CharacterId: ayla.ID.String(), MapId: "limbo"}})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_NO_MAP) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a bad character ID", func(t *testing.T) {
		_, err := s.c.PickUp(s.ctx, &worldv1.PickUpRequest{Presence: &worldv1.Presence{CharacterId: "nope", MapId: "town"}})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_BAD_ID) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLeaveRemovesOnlyThroughTheConnectionThatJoined(t *testing.T) {
	s := newStack(t, 0)
	gw := s.gateway(t, "gw-1")
	ayla, bo := s.character("Ayla"), s.character("Bo")
	s.join(t, "gw-1", "conn-a", ayla)
	s.join(t, "gw-1", "conn-b", bo)
	until(t, gw, "Bo's snapshot", func(m *worldv1.DeliverResponse) bool {
		return m.GetCharacterId() == bo.ID.String() && m.GetSnapshot() != nil
	})
	presence := &worldv1.Presence{CharacterId: ayla.ID.String(), MapId: "town"}
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: presence, ConnectionId: "conn-old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: presence, ConnectionId: "conn-a"}); err != nil {
		t.Fatal(err)
	}
	// Both leaves are drained in order in one tick at most: the first changed
	// nothing, the second removed her, and Bo sees her go once.
	m := until(t, gw, "her removal", func(m *worldv1.DeliverResponse) bool {
		return m.GetCharacterId() == bo.ID.String() && len(m.GetDelta().GetRemovedPlayers()) > 0
	})
	if got := m.GetDelta().GetRemovedPlayers(); len(got) != 1 || got[0] != ayla.ID.String() {
		t.Fatalf("removed %v", got)
	}
	if _, err := s.c.Leave(s.ctx, &worldv1.LeaveRequest{Presence: presence}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("leave without a connection: %v", err)
	}
}

func TestJoinFailures(t *testing.T) {
	s := newStack(t, 0)
	ayla := s.character("Ayla")
	t.Run("no stream for the gateway", func(t *testing.T) {
		_, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-none", ConnectionId: "c", AccountId: s.account.String(), CharacterId: ayla.ID.String()})
		if r, _ := grpcx.ReasonOf(err); r != grpcx.Key(worldv1.Reason_REASON_NOT_SUBSCRIBED) {
			t.Fatalf("err = %v", err)
		}
	})
	s.gateway(t, "gw-1")
	t.Run("someone else's character fails as the character service said", func(t *testing.T) {
		_, err := s.c.Join(s.ctx, &worldv1.JoinRequest{GatewayId: "gw-1", ConnectionId: "c", AccountId: model.NewAccountID().String(), CharacterId: ayla.ID.String()})
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
	s := newStack(t, 4)
	gw := s.gateway(t, "gw-1")
	ayla := s.character("Ayla")
	s.join(t, "gw-1", "conn-1", ayla)
	sink := <-s.sinks
	// The gateway reads nothing: the queue and the transport fill, and then
	// the sink refuses, which drops the player.
	refused := false
	for range 2_000_000 {
		if !sink.Send(world.Chat{Sender: ayla.ID, SenderName: "Ayla", Text: "spam"}) {
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
	if sink.Send(world.Chat{Sender: ayla.ID, Text: "after"}) {
		t.Fatal("a sink on an ended stream still accepts")
	}
	t.Run("a new subscription takes joins again", func(t *testing.T) {
		gw := s.gateway(t, "gw-1")
		s.join(t, "gw-1", "conn-2", ayla)
		until(t, gw, "the snapshot", func(m *worldv1.DeliverResponse) bool { return m.GetSnapshot() != nil })
	})
}

func TestPickupFailuresCrossWithTheirReason(t *testing.T) {
	s := newStack(t, 0)
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
		{"the item service's, passed through", grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL)},
		{"the world's own", world.ErrPickupBusy, grpcx.Key(worldv1.Reason_REASON_PICKUP_BUSY)},
		{"one nobody expected crosses empty", errors.New("disk on fire"), grpcx.Reason{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sink.Send(world.PickupFailed{Tick: 7, Err: c.err})
			m := until(t, gw, "the failure", func(m *worldv1.DeliverResponse) bool { return m.GetPickupFailed() != nil })
			f := m.GetPickupFailed()
			if m.GetTick() != 7 || f.GetErrorDomain() != c.want.Domain || f.GetErrorReason() != c.want.Name {
				t.Fatalf("got %+v", m)
			}
		})
	}
}

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "world"))
	if err != nil || len(declared) < 2 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentinels.Referenced("errors.go")
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
	for _, r := range grpcx.Reasons(worldv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
