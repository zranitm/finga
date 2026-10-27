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
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/application/character"
	"finga/internal/application/chat"
	"finga/internal/application/party"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/handler/ws"
	"finga/internal/model"
	"finga/internal/protocol"
	"finga/internal/sim"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/wsconn"
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
	ts   *httptest.Server
	town *fakeInstance
	obs  *fakeObserver
	gw   *ws.Gateway
}

func newHarness(t *testing.T, opts ...func(*ws.Dependencies)) *harness {
	t.Helper()
	town := newFakeInstance()
	obs := newFakeObserver()
	deps := ws.Dependencies{
		Sessions: &fakeSessions{tokens: map[string]auth.Principal{
			tokenAlice: {AccountID: uuid.UUID(alice), SessionID: uuid.UUID(sessAlice)},
			tokenBob:   {AccountID: uuid.UUID(bob), SessionID: uuid.UUID(sessBob)},
		}},
		Characters: &fakeCharacters{chars: map[model.CharacterID]character.Character{
			aylaID:  {ID: aylaID, AccountID: alice, Name: "Ayla", Class: model.Warrior, Level: 1, HP: 204, MP: 28, MapID: "town", X: 416, Y: 608, Stats: gamedata.Stats{STR: 12}},
			robinID: {ID: robinID, AccountID: alice, Name: "Robin", Class: model.Bowman, Level: 1, HP: 105, MP: 33, MapID: "town", X: 400, Y: 608},
			brunoID: {ID: brunoID, AccountID: bob, Name: "Bruno", Class: model.Warrior, Level: 1, HP: 204, MP: 28, MapID: "town", X: 500, Y: 608},
		}},
		World:        &fakeWorld{instances: map[string]ws.MapInstance{"town": town}},
		Logger:       slog.New(slog.DiscardHandler),
		Observer:     obs,
		HelloTimeout: 200 * time.Millisecond,
	}
	for _, o := range opts {
		o(&deps)
	}
	gw := ws.New(deps)
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	return &harness{ts: ts, town: town, obs: obs, gw: gw}
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
	h.town.mu.Lock()
	joined := h.town.joined
	h.town.mu.Unlock()
	if len(joined) != 1 || joined[0].ID != aylaID || joined[0].HP != 204 || joined[0].Stats.STR != 12 || joined[0].X != 416 {
		t.Fatalf("joined %+v", joined)
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
	h.town.mu.Lock()
	defer h.town.mu.Unlock()
	if len(h.town.joined) != 0 {
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
			h.town.mu.Lock()
			defer h.town.mu.Unlock()
			if len(h.town.joined) != 0 {
				t.Fatal("joined after a failed Hello")
			}
			h.obs.mu.Lock()
			defer h.obs.mu.Unlock()
			if h.obs.online != 0 {
				t.Fatalf("players online %d after a failed Hello", h.obs.online)
			}
		})
	}
	t.Run("a join failure closes without a kick reason", func(t *testing.T) {
		h := newHarness(t)
		h.town.joinErr = world.ErrBusy
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		if _, status := c.read(); status != websocket.StatusInternalError {
			t.Fatalf("status %d", status)
		}
	})
	t.Run("a character on an unknown map", func(t *testing.T) {
		h := newHarness(t, func(d *ws.Dependencies) {
			d.Characters = &fakeCharacters{chars: map[model.CharacterID]character.Character{aylaID: {ID: aylaID, AccountID: alice, MapID: "limbo"}}}
		})
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		if _, status := c.read(); status != websocket.StatusInternalError {
			t.Fatalf("status %d", status)
		}
	})
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
			if h.town.inputCount() != 0 {
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
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 7}})
	c.expect("Pong")
	h.town.mu.Lock()
	defer h.town.mu.Unlock()
	if len(h.town.inputs) != 1 || h.town.inputs[0].id != aylaID || h.town.inputs[0].seq != 5 || !h.town.inputs[0].in.Right {
		t.Fatalf("inputs %+v", h.town.inputs)
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
	time.Sleep(50 * time.Millisecond)
	h.town.mu.Lock()
	left := append([]model.CharacterID(nil), h.town.left...)
	h.town.mu.Unlock()
	if len(left) != 1 || left[0] != aylaID {
		t.Fatalf("left %v, want Ayla only", left)
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
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong") // inputs before it have been handled
	if got := h.town.inputCount(); got != 30 {
		t.Fatalf("%d inputs forwarded of 40 in one instant, want 30 (the burst)", got)
	}
	t.Run("a limited command with a request ID is answered rate_limited", func(t *testing.T) {
		for i := range 6 {
			c.send(wsconn.Message{ReqID: "r", Data: protocol.InventoryMove{IdempotencyKey: "k", ItemID: "x", Tab: "use", Slot: i}})
		}
		var codes []string
		for range 6 {
			m := c.expect("Response")
			codes = append(codes, m.Data.(protocol.Response).Code)
		}
		if codes[5] != "rate_limited" || codes[0] == "rate_limited" {
			t.Fatalf("codes %v", codes)
		}
	})
}

func TestWorldEventsReachTheClient(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.town.push(aylaID, world.Delta{Tick: 9, AckSeq: 3, Players: []world.PlayerView{{ID: robinID, Name: "Robin", Class: model.Bowman, Level: 2, HP: 100, MaxHP: 105, State: sim.State{X: 10, Y: 20, VX: 200, Facing: -1, OnGround: true}}}, Removed: []model.CharacterID{brunoID}})
	m := c.expect("Delta")
	d := m.Data.(protocol.Delta)
	if m.Tick != 9 || d.AckSeq != 3 || len(d.Players) != 1 || d.Players[0].ID != robinID.String() || d.Players[0].VX != 200 || d.Players[0].Facing != -1 || !d.Players[0].OnGround || d.RemovedPlayers[0] != brunoID.String() {
		t.Fatalf("delta %+v (tick %d)", d, m.Tick)
	}
	h.town.push(aylaID, world.Teleport{Tick: 10, MapID: "forest", X: 64, Y: 608})
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.town.mu.Lock()
		n := len(h.town.left)
		h.town.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.town.mu.Lock()
	defer h.town.mu.Unlock()
	if len(h.town.left) != 1 || h.town.left[0] != aylaID {
		t.Fatalf("left %v", h.town.left)
	}
	h.obs.mu.Lock()
	defer h.obs.mu.Unlock()
	if h.obs.online != 0 {
		t.Fatalf("players online %d after close", h.obs.online)
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
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong")
	h.town.mu.Lock()
	defer h.town.mu.Unlock()
	if len(h.town.attacks) != 1 || h.town.attacks[0] != aylaID {
		t.Fatalf("attacks %v", h.town.attacks)
	}
}

func TestCombatEventsReachTheClient(t *testing.T) {
	h := newHarness(t)
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.town.push(aylaID, world.Damage{Tick: 5, Source: world.EntityRef{Player: aylaID}, Target: world.EntityRef{Mob: 7}, Amount: 21, Crit: true})
	m := c.expect("Damage")
	d := m.Data.(protocol.Damage)
	if d.Source != "p:"+aylaID.String() || d.Target != "m:7" || d.Amount != 21 || !d.Crit || m.Tick != 5 {
		t.Fatalf("damage %+v", d)
	}
	h.town.push(aylaID, world.Damage{Tick: 6, Source: world.EntityRef{Mob: 7}, Target: world.EntityRef{Player: aylaID}, Miss: true})
	if d := c.expect("Damage").Data.(protocol.Damage); d.Source != "m:7" || !d.Miss {
		t.Fatalf("damage %+v", d)
	}
	h.town.push(aylaID, world.Died{Tick: 7, ID: aylaID, X: 1, Y: 2})
	if d := c.expect("Died").Data.(protocol.Died); d.CharacterID != aylaID.String() || d.X != 1 {
		t.Fatalf("died %+v", d)
	}
	h.town.push(aylaID, world.Respawned{Tick: 8, ID: aylaID, MapID: "town", X: 416, Y: 608, HP: 102})
	if r := c.expect("Respawned").Data.(protocol.Respawned); r.MapID != "town" || r.HP != 102 {
		t.Fatalf("respawned %+v", r)
	}
	h.town.push(aylaID, world.ExpGained{Tick: 9, Amount: 5, Exp: 15})
	if e := c.expect("ExpGained").Data.(protocol.ExpGained); e.Amount != 5 || e.Exp != 15 {
		t.Fatalf("exp %+v", e)
	}
	h.town.push(aylaID, world.LevelUp{Tick: 10, ID: aylaID, Name: "Ayla", Class: model.Warrior, Level: 2, Exp: 3, Stats: gamedata.Stats{STR: 15, VIT: 12}, HP: 252, MaxHP: 252, MP: 32, MaxMP: 32})
	lu := c.expect("LevelUp").Data.(protocol.LevelUp)
	if lu.Character.Level != 2 || lu.Character.MaxHP != 252 || lu.Character.Stats.STR != 15 || lu.Character.Name != "Ayla" {
		t.Fatalf("level-up %+v", lu)
	}
	h.town.push(aylaID, world.Delta{Tick: 11, Mobs: []world.MobView{{ID: 7, Type: "green_slime", X: 300, Y: 608, Facing: -1, HP: 9, MaxHP: 30}}, RemovedMobs: []uint64{6}})
	dl := c.expect("Delta").Data.(protocol.Delta)
	if len(dl.Mobs) != 1 || dl.Mobs[0].Type != "green_slime" || dl.Mobs[0].HP != 9 || len(dl.RemovedMobs) != 1 || dl.RemovedMobs[0] != 6 {
		t.Fatalf("delta %+v", dl)
	}
}

func TestTeleportRepointsTheSession(t *testing.T) {
	forest := newFakeInstance()
	h := newHarness(t, func(d *ws.Dependencies) {
		d.World = &fakeWorld{instances: map[string]ws.MapInstance{"town": d.World.(*fakeWorld).instances["town"], "forest": forest}}
	})
	c := h.dial(t)
	c.hello(tokenAlice, aylaID)
	c.expect("Snapshot")
	h.town.push(aylaID, world.Teleport{Tick: 3, MapID: "forest", X: 64, Y: 608})
	c.expect("MapChange")
	c.send(wsconn.Message{Seq: 9, Data: protocol.Input{Left: true}})
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong")
	forest.mu.Lock()
	defer forest.mu.Unlock()
	if len(forest.inputs) != 1 || forest.inputs[0].id != aylaID {
		t.Fatalf("forest inputs %+v; the session still points at the town", forest.inputs)
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
		c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
		c.expect("Pong")
		h.town.mu.Lock()
		defer h.town.mu.Unlock()
		if len(h.town.pickups) != 1 || h.town.pickups[0] != aylaID {
			t.Fatalf("pickups %v", h.town.pickups)
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
		h.town.push(aylaID, world.Delta{Tick: 12, Drops: []world.DropView{{ID: 3, Template: "slime_gel", Quantity: 2, X: 10, Y: 20}}, RemovedDrops: []uint64{2}})
		d := c.expect("Delta").Data.(protocol.Delta)
		if len(d.Drops) != 1 || d.Drops[0].Template != "slime_gel" || d.RemovedDrops[0] != 2 {
			t.Fatalf("delta %+v", d)
		}
		h.town.push(aylaID, world.PickupFailed{Tick: 13, Err: grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL)})
		r := c.expect("Response").Data.(protocol.Response)
		if r.OK || r.Code != "inventory_full" || r.Message != "your inventory is full" {
			t.Fatalf("response %+v", r)
		}
		h.town.push(aylaID, world.InventoryChanged{Tick: 14, Slots: []world.Slot{{Tab: gamedata.Etc, Slot: 1, Item: &world.SlotItem{ID: model.NewItemID(), Template: "slime_gel", Quantity: 2}}}})
		ic := c.expect("InventoryChanged").Data.(protocol.InventoryChanged)
		if len(ic.Slots) != 1 || ic.Slots[0].Item.Template != "slime_gel" {
			t.Fatalf("inventory changed %+v", ic)
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
	c.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
	c.expect("Pong")
	h.town.mu.Lock()
	defer h.town.mu.Unlock()
	if len(h.town.portals) != 1 || h.town.portals[0] != aylaID {
		t.Fatalf("portals %v", h.town.portals)
	}
}

func TestChatRouting(t *testing.T) {
	fc := &fakeChat{recipients: []model.CharacterID{aylaID, brunoID}}
	h := newHarness(t, func(d *ws.Dependencies) { d.Chat = fc })
	ayla := h.dial(t)
	ayla.hello(tokenAlice, aylaID)
	ayla.expect("Snapshot")
	bruno := h.dial(t)
	bruno.hello(tokenBob, brunoID)
	bruno.expect("Snapshot")

	t.Run("a map line goes to the sender's map instance with the connection's identity", func(t *testing.T) {
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "hello map"}})
		ayla.send(wsconn.Message{Data: protocol.Ping{ClientTime: 1}})
		ayla.expect("Pong")
		h.town.mu.Lock()
		defer h.town.mu.Unlock()
		if len(h.town.chats) != 1 || h.town.chats[0].id != aylaID || h.town.chats[0].name != "Ayla" || h.town.chats[0].text != "hello map" {
			t.Fatalf("chats %+v", h.town.chats)
		}
	})
	t.Run("a map line from the instance reaches the client as ChatMessage", func(t *testing.T) {
		h.town.push(brunoID, world.Chat{Tick: 3, Sender: aylaID, SenderName: "Ayla", Text: "hello map"})
		cm := bruno.expect("ChatMessage").Data.(protocol.ChatMessage)
		if cm.Channel != protocol.ChatMap || cm.Sender != aylaID.String() || cm.SenderName != "Ayla" || cm.Text != "hello map" {
			t.Fatalf("chat message %+v", cm)
		}
	})
	t.Run("a party line is delivered to every recipient's connection, the sender included", func(t *testing.T) {
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatParty, Text: "psst"}})
		for _, c := range []*client{ayla, bruno} {
			cm := c.expect("ChatMessage").Data.(protocol.ChatMessage)
			if cm.Channel != protocol.ChatParty || cm.Sender != aylaID.String() || cm.Text != "psst" {
				t.Fatalf("party chat %+v", cm)
			}
		}
	})
	t.Run("a refused line answers a Response without a request ID", func(t *testing.T) {
		fc.err = chat.ErrTooLong
		ayla.send(wsconn.Message{Data: protocol.ChatSend{Channel: protocol.ChatMap, Text: "..."}})
		r := ayla.expect("Response")
		if r.ReqID != "" || r.Data.(protocol.Response).OK || r.Data.(protocol.Response).Code != "chat_too_long" {
			t.Fatalf("response %+v", r)
		}
		fc.err = nil
	})
}

func TestPartyCommandsAndState(t *testing.T) {
	fp := &fakeParties{of: map[model.CharacterID]party.Party{
		aylaID: {Leader: aylaID, Members: []party.Member{{ID: aylaID, Name: "Ayla", Level: 1, HP: 204, MaxHP: 204, MapID: "town", Online: true}, {ID: brunoID, Name: "Bruno", Level: 1, MapID: "forest"}}},
	}}
	h := newHarness(t, func(d *ws.Dependencies) { d.Parties = fp })
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
	fp.mu.Lock()
	if len(fp.online) != 2 || fp.online[0].ID != aylaID || fp.online[0].Name != "Ayla" || fp.online[0].MapID != "town" || fp.online[0].MaxHP != 204 || fp.online[0].Level != 1 {
		t.Fatalf("online %+v", fp.online)
	}
	fp.mu.Unlock()

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
			fp.mu.Lock()
			got := fp.calls[len(fp.calls)-1]
			fp.mu.Unlock()
			if got != tt.want {
				t.Fatalf("%T: call %q, want %q", tt.msg, got, tt.want)
			}
		}
	})
	t.Run("errors map to the Response code", func(t *testing.T) {
		ayla.send(wsconn.Message{ReqID: "p-x", Data: protocol.PartyInvite{CharacterID: "nope"}})
		if r := ayla.expect("Response").Data.(protocol.Response); r.OK || r.Code != "bad_id" {
			t.Fatalf("malformed id: %+v", r)
		}
		fp.err = party.ErrNotLeader
		ayla.send(wsconn.Message{ReqID: "p-y", Data: protocol.PartyKick{CharacterID: brunoID.String()}})
		if r := ayla.expect("Response").Data.(protocol.Response); r.OK || r.Code != "not_party_leader" || r.Message == "" {
			t.Fatalf("not leader: %+v", r)
		}
		fp.err = nil
	})
	t.Run("the gateway is the notifier: PartyState and PartyInvited reach the connections", func(t *testing.T) {
		h.gw.PartyChanged([]model.CharacterID{aylaID, brunoID, robinID}, &party.Party{Leader: brunoID, Members: []party.Member{{ID: brunoID, Name: "Bruno", Online: true}, {ID: aylaID, Name: "Ayla", Online: true}}})
		for _, c := range []*client{ayla, bruno} {
			ps := c.expect("PartyState").Data.(protocol.PartyState)
			if ps.Party == nil || ps.Party.Leader != brunoID.String() || len(ps.Party.Members) != 2 {
				t.Fatalf("party state %+v", ps)
			}
		}
		h.gw.Invited(brunoID, party.Invite{ID: "inv-2", From: aylaID, FromName: "Ayla", To: brunoID})
		if inv := bruno.expect("PartyInvited").Data.(protocol.PartyInvited); inv.InviteID != "inv-2" || inv.From != aylaID.String() || inv.FromName != "Ayla" {
			t.Fatalf("invited %+v", inv)
		}
		h.gw.PartyChanged([]model.CharacterID{aylaID}, nil)
		if ps := ayla.expect("PartyState").Data.(protocol.PartyState); ps.Party != nil {
			t.Fatalf("party state after leaving %+v", ps)
		}
	})
	t.Run("closing the connection tells the party service", func(t *testing.T) {
		_ = bruno.c.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			fp.mu.Lock()
			n := len(fp.offline)
			fp.mu.Unlock()
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
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = h.gw.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
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
		d.ID = "gw-test"
		d.Watcher = accountv1.NewAccountServiceClient(grpctest.Conn(t, srv))
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = h.gw.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

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
