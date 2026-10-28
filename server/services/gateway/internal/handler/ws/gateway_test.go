package ws_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/ws"
)

const (
	tokenAlice = "tok-alice"
	tokenBob   = "tok-bob"
)

var (
	alice     = model.NewAccountID()
	bob       = model.NewAccountID()
	aylaID    = model.NewCharacterID()
	robinID   = model.NewCharacterID()
	brunoID   = model.NewCharacterID()
	sessAlice = model.NewSessionID()
	sessBob   = model.NewSessionID()
)

type harness struct {
	ts    *httptest.Server
	world *fakeWorld
	obs   *fakeObserver
	gw    *ws.Gateway
}

// newHarness builds the gateway over a fake world service, runs it, and
// waits until it follows the world's stream.
func newHarness(t *testing.T, opts ...func(*ws.Dependencies)) *harness {
	t.Helper()
	return newHarnessOn(t, newFakeWorld(), opts...)
}

// newHarnessOn is newHarness over a fake world the test has set up.
func newHarnessOn(t *testing.T, fw *fakeWorld, opts ...func(*ws.Dependencies)) *harness {
	t.Helper()
	obs := newFakeObserver()
	deps := ws.Dependencies{
		ID: "gw-test",
		Sessions: &fakeSessions{tokens: map[string]auth.Principal{
			tokenAlice: {AccountID: uuid.UUID(alice), SessionID: uuid.UUID(sessAlice)},
			tokenBob:   {AccountID: uuid.UUID(bob), SessionID: uuid.UUID(sessBob)},
		}},
		World:        fw.client(t),
		Logger:       slog.New(slog.DiscardHandler),
		Observer:     obs,
		HelloTimeout: 200 * time.Millisecond,
		JoinTimeout:  300 * time.Millisecond,
	}
	for _, o := range opts {
		o(&deps)
	}
	gw := ws.New(deps)
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = gw.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-fw.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway never subscribed to the world")
	}
	return &harness{ts: ts, world: fw, obs: obs, gw: gw}
}

// client is a test WebSocket client speaking the envelope.
type client struct {
	t     *testing.T
	c     *websocket.Conn
	codec *wsconn.JSONCodec
}

func (h *harness) dial(t *testing.T) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return &client{t: t, c: c, codec: protocol.NewCodec()}
}

func (c *client) send(m wsconn.Message) {
	c.t.Helper()
	b, err := c.codec.Encode(m)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.c.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) sendRaw(raw string) {
	c.t.Helper()
	if err := c.c.Write(context.Background(), websocket.MessageText, []byte(raw)); err != nil {
		c.t.Fatal(err)
	}
}

// read returns the next message, or the close status when the server closed.
func (c *client) read() (wsconn.Message, websocket.StatusCode) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.c.Read(ctx)
	if err != nil {
		return wsconn.Message{}, websocket.CloseStatus(err)
	}
	m, err := c.codec.Decode(data)
	if err != nil {
		c.t.Fatalf("decode %s: %v", data, err)
	}
	return m, -1
}

// expect reads until a message of the given type, failing on close.
func (c *client) expect(msgType string) wsconn.Message {
	c.t.Helper()
	for {
		m, status := c.read()
		if status != -1 {
			c.t.Fatalf("closed with %d while waiting for %s", status, msgType)
		}
		if m.Type == msgType {
			return m
		}
	}
}

func (c *client) hello(token string, charID model.CharacterID) {
	c.t.Helper()
	c.send(wsconn.Message{Data: protocol.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterID: charID.String()}})
}

// expectKick reads a Kicked message with the reason, then the close.
func (c *client) expectKick(reason protocol.KickReason) {
	c.t.Helper()
	m := c.expect("Kicked")
	if got := m.Data.(protocol.Kicked).Reason; got != reason {
		c.t.Fatalf("kicked for %q, want %q", got, reason)
	}
	if _, status := c.read(); status != websocket.StatusCode(wsconn.CloseKicked) {
		c.t.Fatalf("close status %d after Kicked", status)
	}
}

func TestHelloHappyPath(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	w := c.expect("Welcome")
	welcome := w.Data.(protocol.Welcome)
	if welcome.Character.ID != aylaID.String() || welcome.Character.Name != "Ayla" || welcome.Character.MaxHP != 204 || welcome.MapID != "town" || welcome.ServerTime == 0 {
		t.Fatalf("welcome %+v", welcome)
	}
	if welcome.Inventory == nil {
		t.Fatal("inventory must be an empty array, not null")
	}
	s := c.expect("Snapshot")
	snap := s.Data.(protocol.Snapshot)
	if s.Tick != 1 || len(snap.Players) != 1 || snap.Players[0].ID != aylaID.String() || snap.Players[0].X != 416 || snap.Mobs == nil || snap.Drops == nil {
		t.Fatalf("snapshot %+v (tick %d)", snap, s.Tick)
	}
	joined := h.world.joins()
	if len(joined) != 1 || joined[0].GetCharacterId() != aylaID.String() || joined[0].GetAccountId() != alice.String() || joined[0].GetGatewayId() != "gw-test" || joined[0].GetConnectionId() == "" {
		t.Fatalf("joined %+v", joined)
	}
	if welcome.Character.Stats.STR != 12 {
		t.Fatalf("welcome character %+v", welcome.Character)
	}
	h.obs.mu.Lock()
	defer h.obs.mu.Unlock()
	if h.obs.online != 1 || h.obs.in["Hello"] != 1 || h.obs.out["Welcome"] != 1 || h.obs.out["Snapshot"] != 1 {
		t.Fatalf("observer %+v", h.obs)
	}
}

func TestNoHelloInTimeClosesTheConnection(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	start := time.Now()
	_, status := c.read()
	if status != websocket.StatusCode(wsconn.CloseKicked) {
		t.Fatalf("status %d", status)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("took too long: the Hello timeout is 200 ms here")
	}
	if len(h.world.joins()) != 0 {
		t.Fatal("joined without Hello")
	}
}

func TestHelloFailures(t *testing.T) {
	tests := []struct {
		name   string
		hello  protocol.Hello
		reason protocol.KickReason
	}{
		{"a bad token", protocol.Hello{Token: "nope", ProtocolVersion: protocol.Version, CharacterID: aylaID.String()}, protocol.KickUnauthenticated},
		{"someone else's character", protocol.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterID: brunoID.String()}, protocol.KickUnauthenticated},
		{"an unknown character", protocol.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterID: model.NewCharacterID().String()}, protocol.KickUnauthenticated},
		{"a malformed character ID", protocol.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterID: "ayla"}, protocol.KickUnauthenticated},
		{"an old protocol version", protocol.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version - 1, CharacterID: aylaID.String()}, protocol.KickOutdatedProtocol},
		{"a future protocol version", protocol.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version + 1, CharacterID: aylaID.String()}, protocol.KickOutdatedProtocol},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			c := h.dial(t)
			c.send(wsconn.Message{Data: tt.hello})
			c.expectKick(tt.reason)
			if len(h.world.joins()) != 0 {
				t.Fatal("joined after a failed Hello")
			}
			h.obs.mu.Lock()
			defer h.obs.mu.Unlock()
			if h.obs.online != 0 {
				t.Fatalf("players online %d after a failed Hello", h.obs.online)
			}
		})
	}
	for _, tt := range []struct {
		name string
		err  error
	}{
		// Internal, not unauthenticated, which would log the client out: this
		// way it reconnects.
		{"a full instance", grpcx.Error(codes.Unavailable, worldv1.Reason_REASON_BUSY)},
		{"a character on a map the world does not run", grpcx.Error(codes.NotFound, worldv1.Reason_REASON_NO_MAP)},
		{"a world that cannot answer", status.Error(codes.Unavailable, "connection refused")},
	} {
		t.Run(tt.name+" closes as internal without a Welcome", func(t *testing.T) {
			h := newHarness(t)
			h.world.joinErr = tt.err
			c := h.dial(t)
			c.hello(tokenAlice, aylaID)
			if m, status := c.read(); status != websocket.StatusInternalError {
				t.Fatalf("got %+v, status %d", m, status)
			}
		})
	}
}

func TestMessagesBeforeHelloAreRejected(t *testing.T) {
	tests := []struct {
		name string
		msg  wsconn.Message
	}{
		{"an input", wsconn.Message{Seq: 1, Data: protocol.Input{Right: true}}},
		{"a ping", wsconn.Message{Data: protocol.Ping{ClientTime: 1}}},
		{"an attack", wsconn.Message{Data: protocol.Attack{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			c := h.dial(t)
			c.send(tt.msg)
			if _, status := c.read(); status != websocket.StatusCode(wsconn.CloseKicked) {
				t.Fatalf("status %d", status)
			}
			if h.world.inputCount() != 0 {
				t.Fatal("an input before Hello reached the world")
			}
		})
	}
}

func TestIdentityComesFromTheConnection(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	// A raw Input that smuggles a character_id field: the field is ignored
	// by the codec and the input is routed as Ayla's.
	c.sendRaw(`{"t":"Input","seq":5,"d":{"right":true,"character_id":"` + brunoID.String() + `"}}`)
	waitFor(t, "the input", h.world.inputList, 1)
	h.world.mu.Lock()
	in := append([]*worldv1.InputRequest(nil), h.world.inputs...)
	h.world.mu.Unlock()
	if len(in) != 1 || in[0].GetPresence().GetCharacterId() != aylaID.String() || in[0].GetPresence().GetMapId() != "town" || in[0].GetSeq() != 5 || !in[0].GetKeys().GetRight() {
		t.Fatalf("inputs %+v", in)
	}
	t.Run("a second Hello on an active session is refused", func(t *testing.T) {
		c.hello(tokenAlice, robinID)
		if _, status := c.read(); status != websocket.StatusCode(wsconn.CloseKicked) {
			t.Fatalf("status %d", status)
		}
	})
}

func TestSecondLoginKicksTheFirst(t *testing.T) {
	h := newHarness(t)
	first := h.dial(t)
	first.hello(tokenAlice, aylaID)
	first.expect("Snapshot")
	second := h.dial(t)
	second.hello(tokenAlice, robinID) // same account, another character
	second.expect("Snapshot")
	first.expectKick(protocol.KickLoggedInElsewhere)
	left := waitFor(t, "Ayla's leave", h.world.leaves, 1)
	if left[0].GetPresence().GetCharacterId() != aylaID.String() || left[0].GetConnectionId() != h.world.joins()[0].GetConnectionId() {
		t.Fatalf("left %+v, want Ayla's, through her connection", left)
	}
	// The second session keeps working.
	second.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	second.expect("Pong")
	h.obs.mu.Lock()
	defer h.obs.mu.Unlock()
	if h.obs.online != 1 {
		t.Fatalf("players online %d, want 1", h.obs.online)
	}
}

func TestRateLimits(t *testing.T) {
	frozen := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	h := newHarness(t, func(d *ws.Dependencies) { d.Clock = func() time.Time { return frozen } })
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	for seq := uint32(1); seq <= 40; seq++ {
		c.send(wsconn.Message{Seq: seq, Data: protocol.Input{Right: true}})
	}
	// Commands reach the world in order: once the Attack after them is in,
	// every input before it has been handled.
	c.send(wsconn.Message{Seq: 41, Data: protocol.Attack{}})
	waitFor(t, "the attack", h.world.attackList, 1)
	if got := h.world.inputCount(); got != 30 {
		t.Fatalf("%d inputs forwarded of 40 in one instant, want 30 (the burst)", got)
	}
	t.Run("a limited command with a request ID is answered rate_limited", func(t *testing.T) {
		for i := range 6 {
			c.send(wsconn.Message{ReqID: fmt.Sprintf("r-%d", i), Data: protocol.InventoryMove{IdempotencyKey: "k", ItemID: "x", Tab: "use", Slot: i}})
		}
		// The read loop answers the refused one at once, while the five
		// before it wait on the command queue: match answers by request ID.
		codes := map[string]string{}
		for range 6 {
			m := c.expect("Response")
			codes[m.ReqID] = m.Data.(protocol.Response).Code
		}
		if codes["r-5"] != "rate_limited" || codes["r-0"] != "inventory_unavailable" || len(codes) != 6 {
			t.Fatalf("codes %v", codes)
		}
	})
}

func TestWorldEventsReachTheClient(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 9, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{
		AckSeq: 3, Players: []*worldv1.PlayerView{{Id: robinID.String(), Name: "Robin", Class: "bowman", Level: 2, Hp: 100, MaxHp: 105, X: 10, Y: 20, Vx: 200, Facing: -1, OnGround: true}},
		RemovedPlayers: []string{brunoID.String()},
	}}})
	m := c.expect("Delta")
	d := m.Data.(protocol.Delta)
	if m.Tick != 9 || d.AckSeq != 3 || len(d.Players) != 1 || d.Players[0].ID != robinID.String() || d.Players[0].VX != 200 || d.Players[0].Facing != -1 || !d.Players[0].OnGround || d.RemovedPlayers[0] != brunoID.String() {
		t.Fatalf("delta %+v (tick %d)", d, m.Tick)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 10, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: "forest", X: 64, Y: 608}}})
	mc := c.expect("MapChange")
	if mc.Data.(protocol.MapChange).MapID != "forest" {
		t.Fatalf("map change %+v", mc.Data)
	}
}

func TestCloseLeavesTheMap(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	_ = c.c.Close(websocket.StatusNormalClosure, "bye")
	left := waitFor(t, "the leave", h.world.leaves, 1)
	if p := left[0].GetPresence(); p.GetCharacterId() != aylaID.String() || p.GetMapId() != "town" || left[0].GetConnectionId() != h.world.joins()[0].GetConnectionId() {
		t.Fatalf("left %+v", left)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.obs.mu.Lock()
		online := h.obs.online
		h.obs.mu.Unlock()
		if online == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("players online %d after close", online)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitFor polls get until it has n items.
func waitFor[T any](t *testing.T, what string, get func() []T, n int) []T {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := get()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: have %d of %d", what, len(got), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestUnavailableCommandsAnswerWithAResponse(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{ReqID: "r-1", Data: protocol.PartyLeave{}})
	m := c.expect("Response")
	if m.ReqID != "r-1" || m.Data.(protocol.Response).OK || m.Data.(protocol.Response).Code != "parties_unavailable" {
		t.Fatalf("response %+v", m)
	}
	c.send(wsconn.Message{Seq: 1, Data: protocol.Attack{}}) // no req: silently ignored for now
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong")
}

func TestOriginCheckAppliesToTheGateway(t *testing.T) {
	h := newHarness(t, func(d *ws.Dependencies) { d.CORSOrigins = []string{"http://localhost:5173"} })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.ts.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://evil.example"}}})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("err %v res %v", err, res)
	}
}

func TestWelcomeIsValidJSONForTheClient(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	ctx := context.Background()
	_, data, err := c.c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil || env["t"] != "Welcome" {
		t.Fatalf("welcome frame %s", data)
	}
}

func TestAttackIsRoutedAsTheSender(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{Seq: 3, Data: protocol.Attack{}})
	waitFor(t, "the attack", h.world.attackList, 1)
	h.world.mu.Lock()
	defer h.world.mu.Unlock()
	if len(h.world.attacks) != 1 || h.world.attacks[0].GetCharacterId() != aylaID.String() || h.world.attacks[0].GetMapId() != "town" {
		t.Fatalf("attacks %v", h.world.attacks)
	}
}

func TestCombatEventsReachTheClient(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	me := &worldv1.EntityRef{Ref: &worldv1.EntityRef_CharacterId{CharacterId: aylaID.String()}}
	mob := &worldv1.EntityRef{Ref: &worldv1.EntityRef_MobId{MobId: 7}}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{Source: me, Target: mob, Amount: 21, Crit: true}}})
	m := c.expect("Damage")
	d := m.Data.(protocol.Damage)
	if d.Source != "p:"+aylaID.String() || d.Target != "m:7" || d.Amount != 21 || !d.Crit || m.Tick != 5 {
		t.Fatalf("damage %+v", d)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 6, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{Source: mob, Target: me, Miss: true}}})
	if d := c.expect("Damage").Data.(protocol.Damage); d.Source != "m:7" || !d.Miss {
		t.Fatalf("damage %+v", d)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_Died{Died: &worldv1.Died{CharacterId: aylaID.String(), X: 1, Y: 2}}})
	if d := c.expect("Died").Data.(protocol.Died); d.CharacterID != aylaID.String() || d.X != 1 {
		t.Fatalf("died %+v", d)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 8, Event: &worldv1.DeliverResponse_Respawned{Respawned: &worldv1.Respawned{CharacterId: aylaID.String(), MapId: "town", X: 416, Y: 608, Hp: 102}}})
	if r := c.expect("Respawned").Data.(protocol.Respawned); r.MapID != "town" || r.HP != 102 {
		t.Fatalf("respawned %+v", r)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 9, Event: &worldv1.DeliverResponse_ExpGained{ExpGained: &worldv1.ExpGained{Amount: 5, Exp: 15}}})
	if e := c.expect("ExpGained").Data.(protocol.ExpGained); e.Amount != 5 || e.Exp != 15 {
		t.Fatalf("exp %+v", e)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 10, Event: &worldv1.DeliverResponse_LevelUp{LevelUp: &worldv1.LevelUp{Character: &worldv1.CharacterView{
		Id: aylaID.String(), Name: "Ayla", Class: "warrior", Level: 2, Exp: 3, Stats: &characterv1.Stats{Strength: 15, Vitality: 12}, Hp: 252, MaxHp: 252, Mp: 32, MaxMp: 32,
	}}}})
	lu := c.expect("LevelUp").Data.(protocol.LevelUp)
	if lu.Character.Level != 2 || lu.Character.MaxHP != 252 || lu.Character.Stats.STR != 15 || lu.Character.Name != "Ayla" {
		t.Fatalf("level-up %+v", lu)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 11, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{
		Mobs: []*worldv1.MobView{{Id: 7, Type: "green_slime", X: 300, Y: 608, Facing: -1, Hp: 9, MaxHp: 30}}, RemovedMobs: []uint64{6},
	}}})
	dl := c.expect("Delta").Data.(protocol.Delta)
	if len(dl.Mobs) != 1 || dl.Mobs[0].Type != "green_slime" || dl.Mobs[0].HP != 9 || len(dl.RemovedMobs) != 1 || dl.RemovedMobs[0] != 6 {
		t.Fatalf("delta %+v", dl)
	}
}

func TestTeleportRepointsTheSession(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: "forest", X: 64, Y: 608}}})
	c.expect("MapChange")
	c.send(wsconn.Message{Seq: 9, Data: protocol.Input{Left: true}})
	waitFor(t, "the input", h.world.inputList, 1)
	h.world.mu.Lock()
	defer h.world.mu.Unlock()
	if len(h.world.inputs) != 1 || h.world.inputs[0].GetPresence().GetMapId() != "forest" {
		t.Fatalf("inputs %+v; the session still points at the town", h.world.inputs)
	}
}

func TestInventoryInWelcomeAndCommands(t *testing.T) {
	potion := &itemv1.Item{Id: model.NewItemID().String(), TemplateId: "red_potion", Tab: "use", Slot: 0, Quantity: 5}
	items := &fakeItems{inventory: []*itemv1.Item{potion}}
	h := newHarness(t, func(d *ws.Dependencies) { d.Items = items.client(t) })
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	w := c.expect("Welcome").Data.(protocol.Welcome)
	if len(w.Inventory) != 1 || w.Inventory[0].ItemID != potion.GetId() || w.Inventory[0].Template != "red_potion" || w.Inventory[0].Quantity != 5 {
		t.Fatalf("welcome inventory %+v", w.Inventory)
	}
	c.expect("Snapshot")

	t.Run("PickUp is routed as the sender", func(t *testing.T) {
		c.send(wsconn.Message{Seq: 4, Data: protocol.PickUp{}})
		waitFor(t, "the pickup", h.world.pickupList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if len(h.world.pickups) != 1 || h.world.pickups[0].GetCharacterId() != aylaID.String() {
			t.Fatalf("pickups %v", h.world.pickups)
		}
	})
	t.Run("InventoryMove answers ok and sends the changed slots, with the connection's identity", func(t *testing.T) {
		moved := &itemv1.Item{Id: potion.GetId(), TemplateId: "red_potion", Tab: "use", Slot: 3, Quantity: 5}
		items.changed = []*itemv1.Slot{{Tab: "use", Slot: 0}, {Tab: "use", Slot: 3, Item: moved}}
		c.send(wsconn.Message{ReqID: "r-1", Data: protocol.InventoryMove{IdempotencyKey: "k-1", ItemID: potion.GetId(), Tab: "use", Slot: 3}})
		r := c.expect("Response")
		if r.ReqID != "r-1" || !r.Data.(protocol.Response).OK {
			t.Fatalf("response %+v", r)
		}
		ic := c.expect("InventoryChanged").Data.(protocol.InventoryChanged)
		if len(ic.Slots) != 2 || ic.Slots[0].Item != nil || ic.Slots[1].Slot != 3 || ic.Slots[1].Item.Quantity != 5 {
			t.Fatalf("inventory changed %+v", ic)
		}
		items.mu.Lock()
		defer items.mu.Unlock()
		mv := items.moves
		if len(mv) != 1 || mv[0].GetIdempotencyKey() != "k-1" || mv[0].GetSlot() != 3 || mv[0].GetTab() != "use" || mv[0].GetItemId() != potion.GetId() ||
			mv[0].GetActor().GetCharacterId() != aylaID.String() || mv[0].GetActor().GetAccountId() != alice.String() {
			t.Fatalf("moves %+v", mv)
		}
	})
	t.Run("InventoryDiscard maps a rule error to the Response code", func(t *testing.T) {
		items.err = grpcx.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_QUANTITY)
		c.send(wsconn.Message{ReqID: "r-2", Data: protocol.InventoryDiscard{IdempotencyKey: "k-2", ItemID: potion.GetId(), Quantity: 9}})
		r := c.expect("Response").Data.(protocol.Response)
		if r.OK || r.Code != "bad_quantity" || r.Message == "" {
			t.Fatalf("response %+v", r)
		}
		items.err = nil
	})
	t.Run("a malformed item ID is bad_id without reaching the service", func(t *testing.T) {
		before := len(items.discards)
		c.send(wsconn.Message{ReqID: "r-3", Data: protocol.InventoryDiscard{IdempotencyKey: "k-3", ItemID: "nope", Quantity: 1}})
		r := c.expect("Response").Data.(protocol.Response)
		if r.OK || r.Code != "bad_id" {
			t.Fatalf("response %+v", r)
		}
		if len(items.discards) != before {
			t.Fatal("the service was called")
		}
	})
	t.Run("drops and pickup results reach the client", func(t *testing.T) {
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 12, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{Drops: []*worldv1.DropView{{Id: 3, Template: "slime_gel", Quantity: 2, X: 10, Y: 20}}, RemovedDrops: []uint64{2}}}})
		d := c.expect("Delta").Data.(protocol.Delta)
		if len(d.Drops) != 1 || d.Drops[0].Template != "slime_gel" || d.RemovedDrops[0] != 2 {
			t.Fatalf("delta %+v", d)
		}
		full := grpcx.Key(itemv1.Reason_REASON_INVENTORY_FULL)
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 13, Event: &worldv1.DeliverResponse_PickupFailed{PickupFailed: &worldv1.PickupFailed{ErrorDomain: full.Domain, ErrorReason: full.Name}}})
		r := c.expect("Response").Data.(protocol.Response)
		if r.OK || r.Code != "inventory_full" || r.Message != "your inventory is full" {
			t.Fatalf("response %+v", r)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 14, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{Slots: []*itemv1.Slot{
			{Tab: "etc", Slot: 1, Item: &itemv1.Item{Id: model.NewItemID().String(), TemplateId: "slime_gel", Tab: "etc", Slot: 1, Quantity: 2}},
		}}}})
		ic := c.expect("InventoryChanged").Data.(protocol.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].Item.Template != "slime_gel" {
			t.Fatalf("inventory changed %+v", ic)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 15, Event: &worldv1.DeliverResponse_PickupFailed{PickupFailed: &worldv1.PickupFailed{}}})
		if r := c.expect("Response").Data.(protocol.Response); r.OK || r.Code != "internal" {
			t.Fatalf("a failure the world could not name: %+v", r)
		}
	})
}

func TestAnUnexpectedCommandErrorIsInternalAndLoggedWithItsCause(t *testing.T) {
	logs := &lockedBuffer{}
	// The item service could not explain its failure: it logged the cause
	// and sent Internal, which is all the gateway sees and logs.
	items := &fakeItems{err: fmt.Errorf("discard: %w", errors.New("pq: connection reset"))}
	h := newHarness(t, func(d *ws.Dependencies) {
		d.Items = items.client(t)
		d.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	})
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Welcome")
	c.expect("Snapshot")
	c.send(wsconn.Message{ReqID: "r-1", Data: protocol.InventoryDiscard{IdempotencyKey: "k", ItemID: model.NewItemID().String(), Quantity: 1}})
	r := c.expect("Response").Data.(protocol.Response)
	if r.OK || r.Code != "internal" || strings.Contains(r.Message, "connection reset") {
		t.Fatalf("response %+v; the cause must stay out of it", r)
	}
	got := logs.String()
	if !strings.Contains(got, `"msg":"command failed"`) || !strings.Contains(got, "code = Internal") || !strings.Contains(got, `"command":"inventory"`) || strings.Contains(got, "connection reset") {
		t.Fatalf("log %s, want the command and the service's Internal, without the cause the service kept", got)
	}
}

// lockedBuffer is a log destination the gateway's goroutines can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestInventoryCommandsWithoutTheItemModule(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{ReqID: "r-1", Data: protocol.InventoryDiscard{IdempotencyKey: "k", ItemID: model.NewItemID().String(), Quantity: 1}})
	if r := c.expect("Response").Data.(protocol.Response); r.OK || r.Code != "inventory_unavailable" {
		t.Fatalf("response %+v", r)
	}
}

func TestEnterPortalIsRoutedAsTheSender(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{Seq: 7, Data: protocol.EnterPortal{}})
	waitFor(t, "the portal", h.world.portalList, 1)
	h.world.mu.Lock()
	defer h.world.mu.Unlock()
	if len(h.world.portals) != 1 || h.world.portals[0].GetCharacterId() != aylaID.String() {
		t.Fatalf("portals %v", h.world.portals)
	}
}

// social builds the gateway with a fake social service, waiting for the
// first subscription to it.
func social(t *testing.T, fs *fakeSocial) *harness {
	t.Helper()
	h := newHarness(t, func(d *ws.Dependencies) { d.Social = fs.client(t) })
	select {
	case <-fs.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway never subscribed to the social service")
	}
	return h
}

func TestChatRouting(t *testing.T) {
	fs := newFakeSocial()
	fs.recipients = []model.CharacterID{aylaID, brunoID}
	h := social(t, fs)
	ayla := h.dial(t)
	ayla.hello(tokenAlice, aylaID)
	ayla.expect("Snapshot")
	bruno := h.dial(t)
	bruno.hello(tokenBob, brunoID)
	bruno.expect("Snapshot")

	t.Run("a map line goes to the sender's map with the connection's identity", func(t *testing.T) {
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "hello map"}})
		waitFor(t, "the map line", h.world.sayList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if len(h.world.says) != 1 || h.world.says[0].GetPresence().GetCharacterId() != aylaID.String() || h.world.says[0].GetPresence().GetMapId() != "town" || h.world.says[0].GetSenderName() != "Ayla" || h.world.says[0].GetText() != "hello map" {
			t.Fatalf("says %+v", h.world.says)
		}
	})
	t.Run("a map line from the world reaches the client as ChatMessage", func(t *testing.T) {
		h.world.push(brunoID, &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Chat{Chat: &worldv1.Chat{SenderId: aylaID.String(), SenderName: "Ayla", Text: "hello map"}}})
		cm := bruno.expect("ChatMessage").Data.(protocol.ChatMessage)
		if cm.Channel != protocol.ChatMap || cm.Sender != aylaID.String() || cm.SenderName != "Ayla" || cm.Text != "hello map" {
			t.Fatalf("chat message %+v", cm)
		}
	})
	t.Run("a party line the social service delivers reaches every recipient, the sender included", func(t *testing.T) {
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatParty, Text: "psst"}})
		for _, c := range []*client{ayla, bruno} {
			cm := c.expect("ChatMessage").Data.(protocol.ChatMessage)
			if cm.Channel != protocol.ChatParty || cm.Sender != aylaID.String() || cm.SenderName != "Ayla" || cm.Text != "psst" {
				t.Fatalf("party chat %+v", cm)
			}
		}
	})
	t.Run("a refused line answers a Response without a request ID", func(t *testing.T) {
		fs.mu.Lock()
		fs.chatErr = grpcx.Error(codes.InvalidArgument, socialv1.Reason_REASON_CHAT_TOO_LONG)
		fs.mu.Unlock()
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "..."}})
		r := ayla.expect("Response")
		if r.ReqID != "" || r.Data.(protocol.Response).OK || r.Data.(protocol.Response).Code != "chat_too_long" {
			t.Fatalf("response %+v", r)
		}
	})
}

func TestChatAndPartiesAreUnavailableWithoutTheSocialService(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "hi"}})
	if r := c.expect("Response").Data.(protocol.Response); r.Code != "chat_unavailable" {
		t.Fatalf("chat %+v", r)
	}
	c.send(wsconn.Message{ReqID: "p", Data: protocol.PartyLeave{}})
	if r := c.expect("Response").Data.(protocol.Response); r.Code != "parties_unavailable" {
		t.Fatalf("party %+v", r)
	}
}

func TestPartyCommandsAndState(t *testing.T) {
	fs := newFakeSocial()
	fs.of[aylaID.String()] = &socialv1.Party{LeaderId: aylaID.String(), Members: []*socialv1.Member{
		{Id: aylaID.String(), Name: "Ayla", Level: 1, Hp: 204, MaxHp: 204, MapId: "town", Online: true},
		{Id: brunoID.String(), Name: "Bruno", Level: 1, MapId: "forest"},
	}}
	h := social(t, fs)
	ayla := h.dial(t)
	ayla.hello(tokenAlice, aylaID)
	w := ayla.expect("Welcome").Data.(protocol.Welcome)
	if w.Party == nil || w.Party.Leader != aylaID.String() || len(w.Party.Members) != 2 || w.Party.Members[1].Name != "Bruno" || w.Party.Members[1].MapID != "forest" || w.Party.Members[1].Online {
		t.Fatalf("welcome party %+v", w.Party)
	}
	ayla.expect("Snapshot")
	bruno := h.dial(t)
	bruno.hello(tokenBob, brunoID)
	if w := bruno.expect("Welcome").Data.(protocol.Welcome); w.Party != nil {
		t.Fatalf("bruno has a party in Welcome: %+v", w.Party)
	}
	bruno.expect("Snapshot")
	// A player is registered after their Snapshot goes out, so the two
	// registrations can land in either order, and the second a moment after
	// its Snapshot: wait for both, and find Ayla's by ID.
	deadline := time.Now().Add(5 * time.Second)
	for len(fs.onlineCalls()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("online %+v", fs.onlineCalls())
		}
		time.Sleep(5 * time.Millisecond)
	}
	var mine *socialv1.OnlineRequest
	for _, req := range fs.onlineCalls() {
		if req.GetMember().GetId() == aylaID.String() {
			mine = req
		}
	}
	if m := mine.GetMember(); m.GetName() != "Ayla" || m.GetMapId() != "town" || m.GetMaxHp() != 204 || m.GetLevel() != 1 || mine.GetGatewayId() == "" {
		t.Fatalf("online %+v", fs.onlineCalls())
	}

	t.Run("each command carries the connection's identity and answers its request", func(t *testing.T) {
		for k, tt := range []struct {
			msg  any
			want string
		}{
			{protocol.PartyInvite{CharacterID: brunoID.String()}, "invite " + aylaID.String() + " " + brunoID.String()},
			{protocol.PartyRespond{InviteID: "inv-9", Accept: true}, "respond " + aylaID.String() + " inv-9 yes"},
			{protocol.PartyKick{CharacterID: brunoID.String()}, "kick " + aylaID.String() + " " + brunoID.String()},
			{protocol.PartyPromote{CharacterID: brunoID.String()}, "promote " + aylaID.String() + " " + brunoID.String()},
			{protocol.PartyLeave{}, "leave " + aylaID.String()},
		} {
			req := "p-" + string(rune('a'+k))
			ayla.send(wsconn.Message{ReqID: req, Data: tt.msg})
			r := ayla.expect("Response")
			if r.ReqID != req || !r.Data.(protocol.Response).OK {
				t.Fatalf("%T: response %+v", tt.msg, r)
			}
			if got := fs.lastCall(); got != tt.want {
				t.Fatalf("%T: call %q, want %q", tt.msg, got, tt.want)
			}
		}
	})
	t.Run("errors map to the Response code", func(t *testing.T) {
		ayla.send(wsconn.Message{ReqID: "p-x", Data: protocol.PartyInvite{CharacterID: "nope"}})
		if r := ayla.expect("Response").Data.(protocol.Response); r.OK || r.Code != "bad_id" {
			t.Fatalf("malformed id: %+v", r)
		}
		fs.mu.Lock()
		fs.err = grpcx.Error(codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER)
		fs.mu.Unlock()
		ayla.send(wsconn.Message{ReqID: "p-y", Data: protocol.PartyKick{CharacterID: brunoID.String()}})
		if r := ayla.expect("Response").Data.(protocol.Response); r.OK || r.Code != "not_party_leader" || r.Message == "" {
			t.Fatalf("not leader: %+v", r)
		}
		fs.mu.Lock()
		fs.err = nil
		fs.mu.Unlock()
	})
	t.Run("PartyState and PartyInvited from the social service's stream reach the connections", func(t *testing.T) {
		state := &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{Party: &socialv1.Party{LeaderId: brunoID.String(), Members: []*socialv1.Member{{Id: brunoID.String(), Name: "Bruno", Online: true}, {Id: aylaID.String(), Name: "Ayla", Online: true}}}}}
		for _, id := range []model.CharacterID{aylaID, brunoID, robinID} { // robin is not connected here: dropped
			fs.push(id, &socialv1.DeliverResponse{Event: state})
		}
		for _, c := range []*client{ayla, bruno} {
			ps := c.expect("PartyState").Data.(protocol.PartyState)
			if ps.Party == nil || ps.Party.Leader != brunoID.String() || len(ps.Party.Members) != 2 {
				t.Fatalf("party state %+v", ps)
			}
		}
		fs.push(brunoID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{InviteId: "inv-2", FromId: aylaID.String(), FromName: "Ayla"}}})
		if inv := bruno.expect("PartyInvited").Data.(protocol.PartyInvited); inv.InviteID != "inv-2" || inv.From != aylaID.String() || inv.FromName != "Ayla" {
			t.Fatalf("invited %+v", inv)
		}
		fs.push(aylaID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{}}})
		if ps := ayla.expect("PartyState").Data.(protocol.PartyState); ps.Party != nil {
			t.Fatalf("party state after leaving %+v", ps)
		}
	})
	t.Run("a social restart clears every party and registers everyone again", func(t *testing.T) {
		before := len(fs.onlineCalls())
		fs.restart("boot-2")
		for _, c := range []*client{ayla, bruno} {
			if ps := c.expect("PartyState").Data.(protocol.PartyState); ps.Party != nil {
				t.Fatalf("party state after the restart %+v", ps)
			}
		}
		deadline := time.Now().Add(5 * time.Second)
		for len(fs.onlineCalls()) < before+2 {
			if time.Now().After(deadline) {
				t.Fatalf("online calls %d, want %d", len(fs.onlineCalls()), before+2)
			}
			time.Sleep(10 * time.Millisecond)
		}
		ayla.send(wsconn.Message{Data: protocol.Ping{ClientTime: 9}})
		ayla.expect("Pong") // the socket stayed up
	})
	t.Run("a stream that breaks without a restart changes nothing", func(t *testing.T) {
		fs.restart("boot-2")
		select {
		case <-fs.subscribed:
		case <-time.After(5 * time.Second):
			t.Fatal("no new subscription")
		}
		ayla.send(wsconn.Message{Data: protocol.Ping{ClientTime: 10}})
		if m := ayla.expect("Pong"); m.Type != "Pong" {
			t.Fatalf("got %+v; a PartyState was sent although the parties survived", m)
		}
	})
	t.Run("closing the connection tells the social service", func(t *testing.T) {
		_ = bruno.c.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			fs.mu.Lock()
			n := len(fs.offline)
			fs.mu.Unlock()
			if n == 1 {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("Offline was not called")
	})
}

func TestChatAndPartyWithoutTheServices(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	c.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "hi"}})
	if r := c.expect("Response").Data.(protocol.Response); r.OK || r.Code != "chat_unavailable" {
		t.Fatalf("chat: %+v", r)
	}
	c.send(wsconn.Message{ReqID: "p-1", Data: protocol.PartyLeave{}})
	if r := c.expect("Response"); r.ReqID != "p-1" || r.Data.(protocol.Response).OK || r.Data.(protocol.Response).Code != "parties_unavailable" {
		t.Fatalf("party: %+v", r)
	}
}

func TestWelcomeCarriesTheRotatedToken(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	w := c.expect("Welcome").Data.(protocol.Welcome)
	if w.Token == "" || w.Token == tokenAlice || !strings.HasPrefix(w.Token, "rotated-") {
		t.Fatalf("welcome token %q", w.Token)
	}
}

func TestSecondHelloOnTheSameSessionKicksTheFirst(t *testing.T) {
	h := newHarness(t)
	first := h.dial(t)
	first.hello(tokenAlice, aylaID)
	first.expect("Snapshot")
	second := h.dial(t)
	second.hello(tokenAlice, aylaID) // the same login session, as a retried Hello would be
	second.expect("Snapshot")
	first.expectKick(protocol.KickLoggedInElsewhere)
}

func TestExpiredSessionIsKicked(t *testing.T) {
	sessions := &fakeSessions{tokens: map[string]auth.Principal{tokenAlice: {AccountID: uuid.UUID(alice), SessionID: uuid.UUID(sessAlice)}}}
	h := newHarness(t, func(d *ws.Dependencies) {
		d.Sessions = sessions
		d.SessionCheckInterval = 20 * time.Millisecond
	})
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	time.Sleep(60 * time.Millisecond) // a few checks pass while the session is fine
	sessions.expire(uuid.UUID(sessAlice))
	c.expectKick(protocol.KickSessionExpired)
}

func TestRevokedSessionsAndAccountsAreKicked(t *testing.T) {
	h := newHarness(t)
	t.Run("a session ended by reuse or logout", func(t *testing.T) {
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.gw.KickSession(sessAlice)
		c.expectKick(protocol.KickSessionExpired)
	})
	t.Run("logging out everywhere", func(t *testing.T) {
		c := h.dial(t)
		c.hello(tokenBob, brunoID)
		c.expect("Snapshot")
		h.gw.KickAccount(bob, false)
		c.expectKick(protocol.KickSessionExpired)
		h.gw.KickAccount(bob, false) // nobody left: nothing happens
	})
}

func TestANewLoginElsewhereKicksAsLoggedInElsewhere(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.gw.KickAccount(alice, true)
	c.expectKick(protocol.KickLoggedInElsewhere)
}

// watcher is the account service's WatchSessions, served over an in-memory
// gRPC connection: each subscription gets Subscribed, then whatever the
// test pushes, and ends when the test says so.
type watcher struct {
	accountv1.UnimplementedAccountServiceServer
	mu            sync.Mutex
	subscriptions []string // the gateway IDs that subscribed, in order
	events        chan *accountv1.WatchSessionsResponse
	end           chan struct{}
}

func (w *watcher) WatchSessions(req *accountv1.WatchSessionsRequest, stream grpc.ServerStreamingServer[accountv1.WatchSessionsResponse]) error {
	w.mu.Lock()
	w.subscriptions = append(w.subscriptions, req.GetGatewayId())
	w.mu.Unlock()
	if err := stream.Send(&accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_Subscribed{Subscribed: &accountv1.Subscribed{BootId: "boot"}}}); err != nil {
		return err
	}
	for {
		select {
		case m := <-w.events:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-w.end:
			return status.Error(codes.Unavailable, "restarting")
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (w *watcher) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.subscriptions)
}

func TestTheGatewayFollowsTheAccountServicesSessionEnds(t *testing.T) {
	w := &watcher{events: make(chan *accountv1.WatchSessionsResponse), end: make(chan struct{})}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	accountv1.RegisterAccountServiceServer(srv, w)
	h := newHarness(t, func(d *ws.Dependencies) {
		d.Watcher = accountv1.NewAccountServiceClient(grpctest.Conn(t, srv))
	})

	t.Run("a new login elsewhere kicks as logged_in_elsewhere", func(t *testing.T) {
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		w.events <- &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_AccountRevoked{AccountRevoked: &accountv1.AccountRevoked{
			AccountId: alice.String(), Reason: accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN,
		}}}
		c.expectKick(protocol.KickLoggedInElsewhere)
	})
	t.Run("a session ended by reuse kicks as session_expired", func(t *testing.T) {
		c := h.dial(t)
		c.hello(tokenBob, brunoID)
		c.expect("Snapshot")
		w.events <- &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_SessionRevoked{SessionRevoked: &accountv1.SessionRevoked{
			SessionId: sessBob.String(), Reason: accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE,
		}}}
		c.expectKick(protocol.KickSessionExpired)
	})
	t.Run("after the stream breaks the gateway subscribes again, under its ID", func(t *testing.T) {
		close(w.end)
		deadline := time.Now().Add(5 * time.Second)
		for w.count() < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("subscriptions %v", w.subscriptions)
			}
			time.Sleep(10 * time.Millisecond)
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.subscriptions[0] != "gw-test" || w.subscriptions[1] != "gw-test" {
			t.Fatalf("subscriptions %v", w.subscriptions)
		}
	})
}

func TestDeliveriesBeforeWelcomeWaitAndFollowIt(t *testing.T) {
	h := newHarness(t)
	// The world sends an older presence's Delta, then the Snapshot, then a
	// Delta, all before Join answers: the old Delta is dropped, the rest
	// follow Welcome in order.
	h.world.before = []*worldv1.DeliverResponse{{Tick: 1, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{AckSeq: 99}}}}
	h.world.mu.Lock()
	h.world.tick = 40
	h.world.mu.Unlock()
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	var got []string
	for len(got) < 2 {
		m, status := c.read()
		if status != -1 {
			t.Fatalf("closed with %d after %v", status, got)
		}
		got = append(got, m.Type)
		if m.Type == "Snapshot" && m.Tick != 41 {
			t.Fatalf("snapshot tick %d", m.Tick)
		}
	}
	if got[0] != "Welcome" || got[1] != "Snapshot" {
		t.Fatalf("order %v, want Welcome then the Snapshot", got)
	}
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 42, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{AckSeq: 1}}})
	if d := c.expect("Delta"); d.Data.(protocol.Delta).AckSeq != 1 {
		t.Fatalf("delta %+v: the older presence's came through", d)
	}
}

func TestAfterTheWorldsStreamBreaksEveryPlayerIsJoinedAgain(t *testing.T) {
	h := newHarness(t)
	ayla := h.dial(t)
	ayla.hello(tokenAlice, aylaID)
	ayla.expect("Snapshot")
	bruno := h.dial(t)
	bruno.hello(tokenBob, brunoID)
	bruno.expect("Snapshot")
	h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 500, Event: &worldv1.DeliverResponse_Delta{Delta: &worldv1.Delta{}}})
	if d := ayla.expect("Delta"); d.Tick != 500 {
		t.Fatalf("delta tick %d", d.Tick)
	}
	// Bruno's last checkpoint is in the forest; Ayla's is where she was.
	h.world.save(brunoID, "forest", 64, 608)
	h.world.restart("boot-2")

	snap := ayla.expect("Snapshot")
	if snap.Tick <= 500 {
		t.Fatalf("snapshot tick %d after 500: the ticks went back on the connection", snap.Tick)
	}
	m, status := bruno.read()
	if status != -1 || m.Type != "MapChange" || m.Data.(protocol.MapChange).MapID != "forest" || m.Data.(protocol.MapChange).X != 64 {
		t.Fatalf("bruno got %+v (status %d), want MapChange to the forest first", m, status)
	}
	if m, _ := bruno.read(); m.Type != "Snapshot" {
		t.Fatalf("bruno got %s after MapChange, want the Snapshot", m.Type)
	}
	joins := h.world.joins()
	if len(joins) != 4 || joins[2].GetConnectionId() == "" {
		t.Fatalf("joins %+v", joins)
	}
	for _, c := range []*client{ayla, bruno} {
		c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
		c.expect("Pong") // the socket stayed open
	}
	bruno.send(wsconn.Message{Seq: 1, Data: protocol.Input{Right: true}})
	waitFor(t, "Bruno's input", h.world.inputList, 1)
	h.world.mu.Lock()
	defer h.world.mu.Unlock()
	if in := h.world.inputs; len(in) != 1 || in[0].GetPresence().GetMapId() != "forest" {
		t.Fatalf("inputs %+v: commands must follow the rejoined map", in)
	}
	h.obs.mu.Lock()
	defer h.obs.mu.Unlock()
	if h.obs.online != 2 {
		t.Fatalf("players online %d; a rejoin is not a new player", h.obs.online)
	}
}

func TestAHelloWhileTheWorldRestartsWaitsForIt(t *testing.T) {
	h := newHarness(t)
	h.world.restart("boot-2") // the stream is down until the gateway subscribes again
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Welcome")
	c.expect("Snapshot")
}

// joined dials and says Hello as Ayla, and waits for her Snapshot.
func (h *harness) joined(t *testing.T) *client {
	t.Helper()
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	return c
}

func TestABlockedCommandDoesNotStopTheReadLoop(t *testing.T) {
	h := newHarness(t)
	c := h.joined(t)
	h.world.mu.Lock()
	h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
	h.world.mu.Unlock()
	c.send(wsconn.Message{Seq: 1, Data: protocol.Attack{}})
	if err := <-h.world.blocked; err != nil { // the world holds the attack
		t.Fatal(err)
	}
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 5}})
	if p := c.expect("Pong").Data.(protocol.Pong); p.ClientTime != 5 {
		t.Fatalf("pong %+v", p)
	}
	close(h.world.block)
	if err := <-h.world.blocked; err != nil {
		t.Fatalf("the attack stopped with %v", err)
	}
}

func TestCommandsReachTheServicesInTheOrderTheyArrived(t *testing.T) {
	h := newHarness(t)
	c := h.joined(t)
	c.send(wsconn.Message{Seq: 1, Data: protocol.Input{Right: true}})
	c.send(wsconn.Message{Seq: 2, Data: protocol.Attack{}})
	c.send(wsconn.Message{Seq: 3, Data: protocol.Input{}})
	c.send(wsconn.Message{Seq: 4, Data: protocol.PickUp{}})
	c.send(wsconn.Message{Seq: 5, Data: protocol.EnterPortal{}})
	c.send(wsconn.Message{Seq: 6, Data: protocol.Input{Left: true}})
	got := waitFor(t, "six commands", h.world.callList, 6)
	want := []string{"input 1", "attack", "input 3", "pickup", "portal", "input 6"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Fatalf("calls %v, want %v", got, want)
	}
}

func TestAFullCommandQueueAnswersRateLimited(t *testing.T) {
	h := newHarness(t, func(d *ws.Dependencies) { d.CommandQueue = 2 })
	c := h.joined(t)
	h.world.mu.Lock()
	h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
	h.world.mu.Unlock()
	c.send(wsconn.Message{Seq: 1, Data: protocol.Attack{}})
	if err := <-h.world.blocked; err != nil { // in flight; the queue behind it is empty
		t.Fatal(err)
	}
	c.send(wsconn.Message{Seq: 2, Data: protocol.Input{Right: true}})
	c.send(wsconn.Message{Seq: 3, Data: protocol.Input{}})
	c.send(wsconn.Message{Seq: 4, Data: protocol.Input{Left: true}}) // dropped: no request ID to answer
	c.send(wsconn.Message{ReqID: "p-1", Data: protocol.PartyLeave{}})
	r := c.expect("Response")
	if r.ReqID != "p-1" || r.Data.(protocol.Response).Code != "rate_limited" {
		t.Fatalf("response %+v", r)
	}
	close(h.world.block)
	if err := <-h.world.blocked; err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queued inputs", h.world.inputList, 2)
	time.Sleep(50 * time.Millisecond) // nothing else is coming
	if got := h.world.inputList(); len(got) != 2 || got[0].GetSeq() != 2 || got[1].GetSeq() != 3 {
		t.Fatalf("inputs %+v, want the two that fit", got)
	}
}

func TestClosingTheConnectionCancelsItsCommandInFlight(t *testing.T) {
	h := newHarness(t)
	c := h.joined(t)
	h.world.mu.Lock()
	h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
	h.world.mu.Unlock()
	c.send(wsconn.Message{Seq: 1, Data: protocol.Attack{}})
	if err := <-h.world.blocked; err != nil {
		t.Fatal(err)
	}
	_ = c.c.Close(websocket.StatusNormalClosure, "bye")
	select {
	case err := <-h.world.blocked:
		if status.Code(err) != codes.Canceled && !errors.Is(err, context.Canceled) {
			t.Fatalf("the attack stopped with %v, want canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the attack in flight was never cancelled")
	}
	waitFor(t, "the leave", h.world.leaves, 1)
}

func TestDrainKicksEveryConnectionAsServerRestart(t *testing.T) {
	h := newHarness(t)
	ayla := h.joined(t)
	bruno := h.dial(t)
	bruno.hello(tokenBob, brunoID)
	bruno.expect("Snapshot")
	silent := h.dial(t) // connected, no Hello yet
	drained := make(chan struct{})
	go func() { defer close(drained); h.gw.Drain(context.Background()) }()
	for _, c := range []*client{ayla, bruno, silent} {
		c.expectKick(protocol.KickServerRestart)
	}
	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("Drain did not return once every connection closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.ts.URL, "http"), nil)
	if err == nil || res == nil || res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a new connection while draining: err %v, response %v", err, res)
	}
	if left := waitFor(t, "the leaves", h.world.leaves, 2); len(left) != 2 {
		t.Fatalf("left %+v", left)
	}
}

func TestTheFirstSubscriptionIsNotABreak(t *testing.T) {
	// The world takes joins as soon as it holds the gateway's stream, which
	// can be before the gateway has read Subscribed; here Subscribed arrives
	// while the first join is in flight. One join, one Snapshot.
	fw := newFakeWorld()
	fw.hold = make(chan struct{})
	h := newHarnessOn(t, fw)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Welcome")
	c.expect("Snapshot")
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong")
	time.Sleep(50 * time.Millisecond) // a rejoin would follow at once
	if n := len(h.world.joins()); n != 1 {
		t.Fatalf("%d joins: the first subscription was taken for a break", n)
	}
}
