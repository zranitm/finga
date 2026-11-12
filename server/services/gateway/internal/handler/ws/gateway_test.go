package ws_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	accountv1 "finga/api/gen/account/v1"
	clientv1 "finga/api/gen/client/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/internal/protocol"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/observability/logger"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/application/push"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/clients"
	"finga/services/gateway/internal/handler/ws"
	"finga/services/gateway/internal/model"
)

const (
	tokenAlice = "tok-alice"
	tokenBob   = "tok-bob"
)

var (
	alice     = kernel.NewAccountID()
	bob       = kernel.NewAccountID()
	aylaID    = kernel.NewCharacterID()
	robinID   = kernel.NewCharacterID()
	brunoID   = kernel.NewCharacterID()
	sessAlice = kernel.NewSessionID()
	sessBob   = kernel.NewSessionID()
)

type observer interface {
	send.Observer
	ws.Observer
	session.Observer
}

// gatewayURL is where a client dials; the harness's transport ignores the address.
const gatewayURL = "ws://gateway"

type harness struct {
	http    *http.Client
	world   *fakeWorld
	obs     *fakeObserver
	streams *push.Gateway
}

type deps struct {
	ID                   string
	Sessions             *fakeSessions
	Watcher              clients.SessionWatcher
	World                worldv1.WorldServiceClient
	Items                itemv1.ItemServiceClient
	Social               socialv1.SocialServiceClient
	Logger               *slog.Logger
	Observer             observer
	CORSOrigins          []string
	HelloTimeout         time.Duration
	JoinTimeout          time.Duration
	CommandQueue         int
	SessionCheckInterval time.Duration
	Traces               trace.TracerProvider
}

// build wires d as the composition root does: one registry under both flows.
func (d deps) build() (*push.Gateway, http.Handler) {
	world := clients.NewWorld(d.World, d.Logger, nil)
	social := clients.NewSocial(d.Social, d.Logger, nil)
	recorder := panics.NewRecorder(d.Logger, nil)
	items := clients.NewItems(d.Items)
	sessions := session.NewRegistry(session.Deps{World: world, Social: social, Items: items, Logger: d.Logger, Panics: recorder, Traces: d.Traces, Observer: d.Observer}, session.Options{Gateway: d.ID, JoinTimeout: d.JoinTimeout})
	sends := send.Deps{Sessions: d.Sessions, Registry: sessions, World: world, Items: items, Social: social, Logger: d.Logger, Observer: d.Observer}
	pushes := push.Deps{Sessions: d.Sessions, Registry: sessions, World: world, Social: social, Account: clients.NewSessionEnds(d.Watcher, d.Logger, nil), Logger: d.Logger, Traces: d.Traces}
	return push.New(pushes, push.Options{SessionCheckInterval: d.SessionCheckInterval}), ws.New(send.New(sends, send.Options{JoinTimeout: d.JoinTimeout}), ws.Options{
		Origins: d.CORSOrigins, HelloTimeout: d.HelloTimeout, CommandQueue: d.CommandQueue, Observer: d.Observer, Logger: d.Logger, Panics: recorder, Traces: d.Traces,
	})
}

// newHarness runs the gateway over a fake world and waits until it follows the world's stream.
func newHarness(t *testing.T, opts ...func(*deps)) *harness {
	t.Helper()
	return newHarnessOn(t, newFakeWorld(), opts...)
}

func newHarnessOn(t *testing.T, fw *fakeWorld, opts ...func(*deps)) *harness {
	t.Helper()
	obs := newFakeObserver()
	d := deps{
		ID: "gw-test",
		Sessions: &fakeSessions{tokens: map[string]model.Principal{
			tokenAlice: {AccountID: alice, SessionID: sessAlice},
			tokenBob:   {AccountID: bob, SessionID: sessBob},
		}},
		World:                fw.client(t),
		Items:                (&fakeItems{}).client(t),
		Social:               newFakeSocial().client(t),
		Watcher:              newWatcher(t),
		Logger:               slog.New(slog.DiscardHandler),
		Observer:             obs,
		HelloTimeout:         200 * time.Millisecond,
		JoinTimeout:          time.Second, // room for two jittered backoffs: the stream's resubscribe and the join's retries
		CommandQueue:         64,
		SessionCheckInterval: time.Minute,
		Traces:               traces,
	}
	for _, o := range opts {
		o(&d)
	}
	streams, handler := d.build()
	client := serve(t, handler)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = streams.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-fw.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway never subscribed to the world")
	}
	return &harness{http: client, world: fw, obs: obs, streams: streams}
}

// client speaks one subprotocol: binary frames, or protojson text frames as finga.v1.json.
type client struct {
	t     *testing.T
	c     *websocket.Conn
	codec wsconn.Codec
	frame websocket.MessageType
}

func (h *harness) dial(t *testing.T) *client {
	t.Helper()
	return h.dialAs(t, protocol.SubprotocolProto)
}

func (h *harness) dialAs(t *testing.T, subprotocol string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, gatewayURL, &websocket.DialOptions{HTTPClient: h.http, Subprotocols: []string{subprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	if c.Subprotocol() != subprotocol {
		t.Fatalf("the server chose %q, want %q", c.Subprotocol(), subprotocol)
	}
	if subprotocol == protocol.SubprotocolJSON {
		return &client{t: t, c: c, codec: protocol.ClientCodec(wsconn.JSON), frame: websocket.MessageText}
	}
	return &client{t: t, c: c, codec: protocol.ClientCodec(wsconn.Binary), frame: websocket.MessageBinary}
}

func (c *client) send(m wsconn.Message) {
	c.t.Helper()
	b, err := c.codec.Encode(m)
	if err != nil {
		c.t.Fatal(err)
	}
	c.sendRaw(b)
}

func (c *client) sendRaw(b []byte) {
	c.t.Helper()
	if err := c.c.Write(context.Background(), c.frame, b); err != nil {
		c.t.Fatal(err)
	}
}

// read returns the next message, or the close status when the server closed.
func (c *client) read() (wsconn.Message, websocket.StatusCode) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frame, data, err := c.c.Read(ctx)
	if err != nil {
		status := websocket.CloseStatus(err)
		if status == -1 {
			c.t.Fatalf("read: %v", err)
		}
		return wsconn.Message{}, status
	}
	if frame != c.frame {
		c.t.Fatalf("a %v frame on a %s connection", frame, c.c.Subprotocol())
	}
	m, err := c.codec.Decode(data)
	if err != nil {
		c.t.Fatalf("decode %q: %v", data, err)
	}
	return m, -1
}

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

// expectEach reads until one of each type has come, in whatever order, skipping the rest: a join's Snapshot comes on the world's stream and its PartyState on social's, in no set order.
func (c *client) expectEach(types ...string) map[string]wsconn.Message {
	c.t.Helper()
	got := map[string]wsconn.Message{}
	for len(got) < len(types) {
		m, status := c.read()
		if status != -1 {
			c.t.Fatalf("closed with %d while waiting for %v", status, types)
		}
		if _, seen := got[m.Type]; !seen && slices.Contains(types, m.Type) {
			got[m.Type] = m
		}
	}
	return got
}

func (c *client) hello(token string, charID kernel.CharacterID) {
	c.t.Helper()
	c.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: charID.String()}})
}

func (c *client) expectKick(reason clientv1.KickReason) {
	c.t.Helper()
	m := c.expect("Kicked")
	if got := m.Data.(*clientv1.Kicked).GetReason(); got != reason {
		c.t.Fatalf("kicked for %s, want %s", got, reason)
	}
	if _, status := c.read(); status != websocket.StatusCode(wsconn.CloseKicked) {
		c.t.Fatalf("close status %d after Kicked", status)
	}
}

func TestHelloHappyPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		w := c.expect("Welcome")
		welcome := w.Data.(*clientv1.Welcome)
		if welcome.GetCharacter().GetId() != aylaID.String() || welcome.GetCharacter().GetName() != "Ayla" || welcome.GetCharacter().GetMaxHp() != 204 || welcome.GetMapId() != "town" || welcome.ServerTime == 0 || welcome.GetTickRateHz() != 30 {
			t.Fatalf("welcome %+v", welcome)
		}
		s := c.expect("Snapshot")
		snap := s.Data.(*clientv1.Snapshot)
		if s.Tick != 1 || len(snap.Players) != 1 || snap.Players[0].GetId() != aylaID.String() || snap.Players[0].X != 416 {
			t.Fatalf("snapshot %+v (tick %d)", snap, s.Tick)
		}
		joined := h.world.joins()
		if len(joined) != 1 || joined[0].GetCharacterId() != aylaID.String() || joined[0].GetAccountId() != alice.String() || joined[0].GetGatewayId() != "gw-test" || joined[0].GetConnectionId() == "" {
			t.Fatalf("joined %+v", joined)
		}
		if welcome.GetCharacter().GetStats().GetStr() != 12 {
			t.Fatalf("welcome character %+v", welcome.Character)
		}
		online := h.obs.onlineAfter(t, 1)
		h.obs.mu.Lock()
		defer h.obs.mu.Unlock()
		if online != 1 || h.obs.in["Hello"] != 1 || h.obs.out["Welcome"] != 1 || h.obs.out["Snapshot"] != 1 {
			t.Fatalf("online %d, in %v, out %v", online, h.obs.in, h.obs.out)
		}
	})
}

func TestNoHelloInTimeClosesTheConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		start := time.Now()
		_, status := c.read()
		if status != websocket.StatusCode(wsconn.CloseKicked) {
			t.Fatalf("status %d", status)
		}
		if waited := time.Since(start); waited != 200*time.Millisecond {
			t.Fatalf("closed after %v, want the Hello timeout of 200 ms", waited)
		}
		if len(h.world.joins()) != 0 {
			t.Fatal("joined without Hello")
		}
	})
}

func TestHelloFailures(t *testing.T) {
	tests := []struct {
		name   string
		hello  *clientv1.Hello
		reason clientv1.KickReason
	}{
		{"a bad token", &clientv1.Hello{Token: "nope", ProtocolVersion: protocol.Version, CharacterId: aylaID.String()}, clientv1.KickReason_KICK_REASON_UNAUTHENTICATED},
		{"someone else's character", &clientv1.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterId: brunoID.String()}, clientv1.KickReason_KICK_REASON_UNAUTHENTICATED},
		{"an unknown character", &clientv1.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterId: kernel.NewCharacterID().String()}, clientv1.KickReason_KICK_REASON_UNAUTHENTICATED},
		{"a malformed character ID", &clientv1.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version, CharacterId: "ayla"}, clientv1.KickReason_KICK_REASON_UNAUTHENTICATED},
		{"an old protocol version", &clientv1.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version - 1, CharacterId: aylaID.String()}, clientv1.KickReason_KICK_REASON_OUTDATED_PROTOCOL},
		{"a future protocol version", &clientv1.Hello{Token: tokenAlice, ProtocolVersion: protocol.Version + 1, CharacterId: aylaID.String()}, clientv1.KickReason_KICK_REASON_OUTDATED_PROTOCOL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
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
		})
	}
	for _, tt := range []struct {
		name string
		err  error
	}{
		// Internal, not unauthenticated (which would log the client out), so it reconnects.
		{"a full instance", grpctest.Error(codes.Unavailable, worldv1.Reason_REASON_BUSY)},
		{"a character on a map the world does not run", grpctest.Error(codes.NotFound, worldv1.Reason_REASON_NO_MAP)},
		{"a world that cannot answer", status.Error(codes.Unavailable, "connection refused")},
	} {
		t.Run(tt.name+" closes as internal without a Welcome", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t)
				h.world.joinErr = tt.err
				c := h.dial(t)
				c.hello(tokenAlice, aylaID)
				if m, status := c.read(); status != websocket.StatusInternalError {
					t.Fatalf("got %+v, status %d", m, status)
				}
			})
		})
	}
}

func TestMessagesBeforeHelloAreRejected(t *testing.T) {
	tests := []struct {
		name string
		msg  wsconn.Message
	}{
		{"an input", wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}}},
		{"a ping", wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}}},
		{"an attack", wsconn.Message{Data: &clientv1.Attack{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
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
		})
	}
}

func TestIdentityComesFromTheConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dialAs(t, protocol.SubprotocolJSON)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		// A smuggled character_id is ignored; the input is routed as Ayla's.
		c.sendRaw([]byte(`{"seq":5,"input":{"right":true,"character_id":"` + brunoID.String() + `"}}`))
		waitFor(t, "the input", h.world.inputList, 1)
		h.world.mu.Lock()
		in := append([]*worldv1.InputRequest(nil), h.world.inputs...)
		h.world.mu.Unlock()
		if len(in) != 1 || in[0].GetPresence().GetCharacterId() != aylaID.String() || in[0].GetPresence().GetConnectionId() != h.world.joins()[0].GetConnectionId() || in[0].GetSeq() != 5 || !in[0].GetKeys().GetRight() {
			t.Fatalf("inputs %+v", in)
		}
		part(t, "a second Hello on an active session is refused", func() {
			c.hello(tokenAlice, robinID)
			if _, status := c.read(); status != websocket.StatusCode(wsconn.CloseKicked) {
				t.Fatalf("status %d", status)
			}
		})
	})
}

func TestSecondLoginKicksTheFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		first := h.dial(t)
		first.hello(tokenAlice, aylaID)
		first.expect("Snapshot")
		second := h.dial(t)
		second.hello(tokenAlice, robinID) // same account, another character
		second.expect("Snapshot")
		first.expectKick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
		left := waitFor(t, "Ayla's leave", h.world.leaves, 1)
		if left[0].GetPresence().GetCharacterId() != aylaID.String() || left[0].GetPresence().GetConnectionId() != h.world.joins()[0].GetConnectionId() {
			t.Fatalf("left %+v, want Ayla's, through her connection", left)
		}
		second.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}})
		second.expect("Pong")
		// Ayla counted, Robin counted, Ayla's connection closed.
		if online := h.obs.onlineAfter(t, 3); online != 1 {
			t.Fatalf("players online %d, want 1", online)
		}
	})
}

// The exact burst at one instant is checked in package receive, under a fake clock.
func TestRateLimits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fw := newFakeWorld()
		fw.tickRate = 20
		h := newHarnessOn(t, fw)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		for seq := uint32(1); seq <= 40; seq++ {
			c.send(wsconn.Message{Seq: seq, Data: &clientv1.Input{Right: true}})
		}
		// Commands reach the world in order: once the Attack is in, every input before it was handled.
		c.send(wsconn.Message{Seq: 41, Data: &clientv1.Attack{}})
		waitFor(t, "the attack", h.world.attackList, 1)
		if got := h.world.inputCount(); got < 30 || got >= 40 {
			t.Fatalf("%d inputs forwarded of 40 sent at once, want the burst of 30 and what refilled meanwhile", got)
		}
		part(t, "a limited command with a request ID is answered RATE_LIMITED", func() {
			for i := range 6 {
				c.send(wsconn.Message{ReqID: fmt.Sprintf("r-%d", i), Data: &clientv1.InventoryMove{IdempotencyKey: "k", ItemId: kernel.NewItemID().String(), Tab: "use", Slot: int32(i)}})
			}
			// The refused one is answered at once on the read loop, ahead of the queue: match by request ID.
			codes := map[string]int32{} // 0 is ok
			for range 6 {
				m := c.expect("Response")
				r := m.Data.(*clientv1.Response)
				codes[m.ReqID] = r.Code
			}
			if codes["r-5"] != int32(model.RateLimited.Code) || codes["r-0"] != 0 || len(codes) != 6 {
				t.Fatalf("codes %v", codes)
			}
		})
	})
}

// The input limit follows the world's tick rate, so one input a tick always passes, at any rate.
func TestTheInputLimitFollowsTheTickRate(t *testing.T) {
	for _, hz := range []uint32{20, 60} {
		t.Run(fmt.Sprintf("%d Hz", hz), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fw := newFakeWorld()
				fw.tickRate = hz
				h := newHarnessOn(t, fw)
				c := h.dial(t)
				c.hello(tokenAlice, aylaID)
				c.expect("Snapshot")
				n := 2 * int(hz)
				for seq := range uint32(n) {
					c.send(wsconn.Message{Seq: seq + 1, Data: &clientv1.Input{Right: true}})
					time.Sleep(time.Second / time.Duration(hz))
				}
				c.send(wsconn.Message{Data: &clientv1.Attack{}})
				waitFor(t, "the attack", h.world.attackList, 1)
				if got := h.world.inputCount(); got != n {
					t.Fatalf("%d of %d inputs, one a tick, reached the world", got, n)
				}
			})
		})
	}
}

func TestWorldEventsReachTheClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.world.delta(aylaID, 9, &worldv1.Delta{
			AckSeq: 3, Players: []*worldv1.PlayerView{{Id: robinID.String(), Name: "Robin", Class: "bowman", Level: 2, Hp: 100, MaxHp: 105, X: 10, Y: 20, Vx: 200, Facing: -1, OnGround: true}},
			RemovedPlayers: []string{brunoID.String()},
		})
		m := c.expect("Delta")
		d := m.Data.(*clientv1.Delta)
		if m.Tick != 9 || d.AckSeq != 3 || len(d.Players) != 1 || d.Players[0].GetId() != robinID.String() || d.Players[0].GetVx() != 200 || d.Players[0].Facing != -1 || !d.Players[0].OnGround || d.RemovedPlayers[0] != brunoID.String() {
			t.Fatalf("delta %+v (tick %d)", d, m.Tick)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 10, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: "forest", X: 64, Y: 608}}})
		mc := c.expect("MapChange")
		if mc.Data.(*clientv1.MapChange).GetMapId() != "forest" {
			t.Fatalf("map change %+v", mc.Data)
		}
	})
}

func TestCloseLeavesTheMap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		_ = c.c.Close(websocket.StatusNormalClosure, "bye")
		left := waitFor(t, "the leave", h.world.leaves, 1)
		if p := left[0].GetPresence(); p.GetCharacterId() != aylaID.String() || p.GetConnectionId() != h.world.joins()[0].GetConnectionId() {
			t.Fatalf("left %+v", left)
		}
		synctest.Wait()
		h.obs.mu.Lock()
		defer h.obs.mu.Unlock()
		if h.obs.online != 0 {
			t.Fatalf("players online %d after close", h.obs.online)
		}
	})
}

// Deliveries already on their way to a connection that closed are not taken for a ghost's: its close leaves once.
func TestACloseLeavesTheMapOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.joined(t)
		_ = c.c.Close(websocket.StatusNormalClosure, "bye")
		waitFor(t, "the leave", h.world.leaves, 1)
		for range 3 {
			h.world.delta(aylaID, 9, &worldv1.Delta{})
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if left := h.world.leaves(); len(left) != 1 {
			t.Fatalf("leaves %v, want the close's one", left)
		}
	})
}

// waitFor lets every goroutine run until it blocks, then expects at least n of what get lists.
func waitFor[T any](t *testing.T, what string, get func() []T, n int) []T {
	t.Helper()
	synctest.Wait()
	got := get()
	if len(got) < n {
		t.Fatalf("waiting for %s: have %d of %d", what, len(got), n)
	}
	return got
}

// part runs one named step of a test whose steps share a harness; a bubble cannot run subtests.
func part(t *testing.T, name string, f func()) {
	t.Helper()
	t.Log(name)
	f()
}

func TestARefusedCommandAnswersWithAResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		soc := newFakeSocial()
		soc.err = grpctest.Error(codes.NotFound, socialv1.Reason_REASON_NO_PARTY)
		h := newHarness(t, func(d *deps) { d.Social = soc.client(t) })
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		c.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.PartyLeave{}})
		m := c.expect("Response")
		if m.ReqID != "r-1" || m.Data.(*clientv1.Response).GetOk() || m.Data.(*clientv1.Response).Code != int32(model.NoParty.Code) {
			t.Fatalf("response %+v", m)
		}
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Attack{}}) // no request ID: silently ignored
		c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}})
		c.expect("Pong")
	})
}

func TestOriginCheckAppliesToTheGateway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(d *deps) { d.CORSOrigins = []string{"http://localhost:5173"} })
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, res, err := websocket.Dial(ctx, gatewayURL, &websocket.DialOptions{HTTPClient: h.http, HTTPHeader: http.Header{"Origin": []string{"http://evil.example"}}})
		if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
			t.Fatalf("err %v res %v", err, res)
		}
	})
}

func TestTheJSONSubprotocolSpeaksProtojsonInTextFrames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dialAs(t, protocol.SubprotocolJSON)
		c.sendRaw(fmt.Appendf(nil, `{"hello":{"token":%q,"protocol_version":%d,"character_id":%q}}`, tokenAlice, protocol.Version, aylaID))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		frame, data, err := c.c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			Welcome *struct {
				MapID     string `json:"map_id"`
				Character struct {
					MaxHP int `json:"max_hp"`
				} `json:"character"`
			} `json:"welcome"`
		}
		if err := json.Unmarshal(data, &env); err != nil || frame != websocket.MessageText || env.Welcome == nil || env.Welcome.MapID != "town" || env.Welcome.Character.MaxHP != 204 {
			t.Fatalf("a %v frame %s (%v), want Welcome as protojson text with the .proto field names", frame, data, err)
		}
	})
}

func TestAClientWithoutAKnownSubprotocolIsClosedAsAPolicyViolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, asked := range [][]string{nil, {"finga.v0.json"}} {
			c, _, err := websocket.Dial(ctx, gatewayURL, &websocket.DialOptions{HTTPClient: h.http, Subprotocols: asked})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.Read(ctx)
			if ce, ok := errors.AsType[websocket.CloseError](err); !ok || ce.Code != websocket.StatusCode(wsconn.CloseNoSubprotocol) || !strings.Contains(ce.Reason, protocol.SubprotocolProto) {
				t.Fatalf("asking for %v: read %v, want a %d close naming %s", asked, err, wsconn.CloseNoSubprotocol, protocol.SubprotocolProto)
			}
			_ = c.CloseNow()
		}
		h.obs.mu.Lock()
		defer h.obs.mu.Unlock()
		if len(h.obs.in) != 0 || len(h.obs.out) != 0 {
			t.Fatalf("in %v, out %v: a connection without a codec reached the handler", h.obs.in, h.obs.out)
		}
	})
}

func TestAttackIsRoutedAsTheSender(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		c.send(wsconn.Message{Seq: 3, Data: &clientv1.Attack{}})
		waitFor(t, "the attack", h.world.attackList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if len(h.world.attacks) != 1 || h.world.attacks[0].GetCharacterId() != aylaID.String() || h.world.attacks[0].GetConnectionId() != h.world.joined[0].GetConnectionId() {
			t.Fatalf("attacks %v", h.world.attacks)
		}
	})
}

func TestCombatEventsReachTheClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		me := &worldv1.EntityRef{Ref: &worldv1.EntityRef_CharacterId{CharacterId: aylaID.String()}}
		mob := &worldv1.EntityRef{Ref: &worldv1.EntityRef_MobId{MobId: 7}}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 5, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{Source: me, Target: mob, Amount: 21, Crit: true}}})
		m := c.expect("Damage")
		d := m.Data.(*clientv1.Damage)
		if d.Source != "p:"+aylaID.String() || d.Target != "m:7" || d.Amount != 21 || !d.Crit || m.Tick != 5 {
			t.Fatalf("damage %+v", d)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 6, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{Source: mob, Target: me, Miss: true}}})
		if d := c.expect("Damage").Data.(*clientv1.Damage); d.Source != "m:7" || !d.Miss {
			t.Fatalf("damage %+v", d)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_SkillUsed{SkillUsed: &worldv1.SkillUsed{Caster: aylaID.String(), Skill: "strike", Stage: 1, Facing: -1}}})
		if u := c.expect("SkillUsed").Data.(*clientv1.SkillUsed); u.GetCaster() != aylaID.String() || u.GetSkill() != "strike" || u.GetStage() != 1 || u.GetFacing() != -1 {
			t.Fatalf("skill used %+v", u)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_AttackUsed{AttackUsed: &worldv1.AttackUsed{Attacker: aylaID.String(), Facing: 1, LengthTicks: 18}}})
		if a := c.expect("AttackUsed").Data.(*clientv1.AttackUsed); a.GetAttacker() != aylaID.String() || a.GetFacing() != 1 || a.GetLengthTicks() != 18 {
			t.Fatalf("attack used %+v", a)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_FieldBoss{FieldBoss: &worldv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 600000}}})
		if b := c.expect("FieldBoss").Data.(*clientv1.FieldBoss); b.GetMobId() != 9 || b.GetType() != "king_slime" || b.GetLeavesInMs() != 600000 {
			t.Fatalf("field boss %+v", b)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_FieldBossGone{FieldBossGone: &worldv1.FieldBossGone{MobId: 9}}})
		if g := c.expect("FieldBossGone").Data.(*clientv1.FieldBossGone); g.GetMobId() != 9 || g.GetBeaten() {
			t.Fatalf("field boss gone %+v", g)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 7, Event: &worldv1.DeliverResponse_Died{Died: &worldv1.Died{CharacterId: aylaID.String(), X: 1, Y: 2}}})
		if d := c.expect("Died").Data.(*clientv1.Died); d.GetCharacterId() != aylaID.String() || d.X != 1 {
			t.Fatalf("died %+v", d)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 8, Event: &worldv1.DeliverResponse_Respawned{Respawned: &worldv1.Respawned{CharacterId: aylaID.String(), MapId: "town", X: 416, Y: 608, Hp: 102}}})
		if r := c.expect("Respawned").Data.(*clientv1.Respawned); r.GetMapId() != "town" || r.GetHp() != 102 {
			t.Fatalf("respawned %+v", r)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 9, Event: &worldv1.DeliverResponse_ExpGained{ExpGained: &worldv1.ExpGained{Amount: 5, Exp: 15}}})
		if e := c.expect("ExpGained").Data.(*clientv1.ExpGained); e.Amount != 5 || e.Exp != 15 {
			t.Fatalf("exp %+v", e)
		}
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 10, Event: &worldv1.DeliverResponse_LevelUp{LevelUp: &worldv1.LevelUp{Character: &worldv1.CharacterView{
			Id: aylaID.String(), Name: "Ayla", Class: "warrior", Level: 2, Exp: 3, Stats: &worldv1.Stats{Strength: 15, Vitality: 12}, Hp: 252, MaxHp: 252, Mp: 32, MaxMp: 32,
		}}}})
		lu := c.expect("LevelUp").Data.(*clientv1.LevelUp)
		if lu.GetCharacter().GetLevel() != 2 || lu.GetCharacter().GetMaxHp() != 252 || lu.GetCharacter().GetStats().GetStr() != 15 || lu.GetCharacter().GetName() != "Ayla" {
			t.Fatalf("level-up %+v", lu)
		}
		h.world.delta(aylaID, 11, &worldv1.Delta{
			Mobs: []*worldv1.MobView{{Id: 7, Type: "green_slime", X: 300, Y: 608, Facing: -1, Hp: 9, MaxHp: 30}}, RemovedMobs: []uint64{6},
		})
		dl := c.expect("Delta").Data.(*clientv1.Delta)
		if len(dl.Mobs) != 1 || dl.Mobs[0].Type != "green_slime" || dl.Mobs[0].GetHp() != 9 || len(dl.RemovedMobs) != 1 || dl.RemovedMobs[0] != 6 {
			t.Fatalf("delta %+v", dl)
		}
	})
}

func TestCommandsAfterATeleportStillCarryTheConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: "forest", X: 64, Y: 608}}})
		c.expect("MapChange")
		c.send(wsconn.Message{Seq: 9, Data: &clientv1.Input{Left: true}})
		waitFor(t, "the input", h.world.inputList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if len(h.world.inputs) != 1 || h.world.inputs[0].GetPresence().GetConnectionId() != h.world.joined[0].GetConnectionId() {
			t.Fatalf("inputs %+v", h.world.inputs)
		}
	})
}

func TestInventoryInWelcomeAndCommands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		potion := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "red_potion", Tab: "use", Slot: 0, Quantity: 5}
		items := &fakeItems{inventory: []*itemv1.Item{potion}, revision: 4}
		h := newHarness(t, func(d *deps) { d.Items = items.client(t) })
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		w := c.expect("Welcome").Data.(*clientv1.Welcome)
		if len(w.Inventory) != 1 || w.Inventory[0].GetItemId() != potion.GetId() || w.Inventory[0].Template != "red_potion" || w.Inventory[0].Quantity != 5 || w.InventoryRevision != 4 {
			t.Fatalf("welcome inventory %+v at revision %d, want the potion at 4", w.Inventory, w.InventoryRevision)
		}
		c.expect("Snapshot")

		part(t, "PickUp, retired, reaches nothing and is answered with nothing", func() {
			c.send(wsconn.Message{Seq: 4, Data: &clientv1.PickUp{}})
			c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}})
			c.expect("Pong")
			if got := h.world.pickupList(); len(got) != 0 {
				t.Fatalf("pickups %v reached the world", got)
			}
		})
		part(t, "InventoryMove answers ok and sends the changed slots at their revision, with the connection's identity", func() {
			moved := &itemv1.Item{Id: potion.GetId(), TemplateId: "red_potion", Tab: "use", Slot: 3, Quantity: 5}
			items.mu.Lock()
			items.changed, items.revision = []*itemv1.Slot{{Tab: "use", Slot: 0}, {Tab: "use", Slot: 3, Item: moved}}, 5
			items.mu.Unlock()
			c.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.InventoryMove{IdempotencyKey: "k-1", ItemId: potion.GetId(), Tab: "use", Slot: 3}})
			r := c.expect("Response")
			if r.ReqID != "r-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v", r)
			}
			ic := c.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
			if len(ic.Slots) != 2 || ic.Slots[0].Item != nil || ic.Slots[1].Slot != 3 || ic.Slots[1].GetItem().GetQuantity() != 5 || ic.Revision != 5 {
				t.Fatalf("inventory changed %+v, want both slots at revision 5", ic)
			}
			items.mu.Lock()
			defer items.mu.Unlock()
			mv := items.moves
			if len(mv) != 1 || mv[0].GetIdempotencyKey() != "k-1" || mv[0].GetSlot() != 3 || mv[0].GetTab() != "use" || mv[0].GetItemId() != potion.GetId() ||
				mv[0].GetActor().GetCharacterId() != aylaID.String() || mv[0].GetActor().GetAccountId() != alice.String() {
				t.Fatalf("moves %+v", mv)
			}
		})
		part(t, "InventoryDiscard maps a rule error to the Response code", func() {
			items.err = grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_QUANTITY)
			c.send(wsconn.Message{ReqID: "r-2", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k-2", ItemId: potion.GetId(), Quantity: 9}})
			r := c.expect("Response").Data.(*clientv1.Response)
			if r.GetOk() || r.Code != int32(model.BadQuantity.Code) {
				t.Fatalf("response %+v", r)
			}
			items.err = nil
		})
		part(t, "a malformed item ID is BAD_ID without reaching the service", func() {
			before := len(items.discards)
			c.send(wsconn.Message{ReqID: "r-3", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k-3", ItemId: "nope", Quantity: 1}})
			r := c.expect("Response").Data.(*clientv1.Response)
			if r.GetOk() || r.Code != int32(model.BadID.Code) {
				t.Fatalf("response %+v", r)
			}
			if len(items.discards) != before {
				t.Fatal("the service was called")
			}
		})
		part(t, "drops, where they fly, and what became of them reach the client", func() {
			h.world.delta(aylaID, 12, &worldv1.Delta{Drops: []*worldv1.DropView{{Id: 3, Template: "slime_gel", Quantity: 2, X: 10, Y: 20, OwnerId: aylaID.String()}}, RemovedDrops: []uint64{2}, CollectedDrops: []uint64{1}})
			d := c.expect("Delta").Data.(*clientv1.Delta)
			if len(d.Drops) != 1 || d.Drops[0].Template != "slime_gel" || d.Drops[0].OwnerId != aylaID.String() || d.RemovedDrops[0] != 2 || len(d.CollectedDrops) != 1 || d.CollectedDrops[0] != 1 {
				t.Fatalf("delta %+v", d)
			}
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 13, Event: &worldv1.DeliverResponse_Loot{Loot: &worldv1.Loot{
				Kind: worldv1.LootKind_LOOT_KIND_COLLECTED, MapId: "forest", DropId: 1, OwnerId: aylaID.String(), OwnerName: "Ayla", Template: "slime_gel", Quantity: 2,
			}}})
			want := &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_COLLECTED, MapId: "forest", DropId: 1, OwnerId: aylaID.String(), OwnerName: "Ayla", Template: "slime_gel", Quantity: 2}
			if l := c.expect("Loot"); l.Tick == 0 || !proto.Equal(l.Data.(*clientv1.Loot), want) {
				t.Fatalf("loot %+v, want %v", l, want)
			}
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 14, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{Revision: 6, Slots: []*worldv1.Slot{
				{Tab: "etc", Slot: 1, Item: &worldv1.Item{Id: kernel.NewItemID().String(), TemplateId: "slime_gel", Tab: "etc", Slot: 1, Quantity: 2}},
			}}}})
			ic := c.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
			if len(ic.Slots) != 1 || ic.Slots[0].GetItem().GetTemplate() != "slime_gel" || ic.Revision != 6 {
				t.Fatalf("inventory changed %+v, want the gel at revision 6", ic)
			}
		})
	})
}

func TestEquipmentInWelcomeAndCommands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		potion := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "red_potion", Tab: "use", Slot: 0, Quantity: 5}
		leatherCap := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "leather_cap", Tab: "equip", Slot: 2, Quantity: 1, Stats: &itemv1.ItemStats{PhysicalDefense: 3, MaxHp: 30}}
		sword := &itemv1.Item{Id: kernel.NewItemID().String(), TemplateId: "wooden_sword", Quantity: 1, EquipSlot: "weapon", Stats: &itemv1.ItemStats{PhysicalAttack: 10}}
		items := &fakeItems{inventory: []*itemv1.Item{potion, leatherCap, sword}, revision: 4}
		h := newHarness(t, func(d *deps) { d.Items = items.client(t) })
		h.world.mu.Lock()
		h.world.chars[aylaID].view.Sheet = &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 12, Vitality: 10}, PrimaryWorn: &worldv1.Stats{},
			Derived: &worldv1.DerivedStats{PhysicalAttack: 34.5, PhysicalDefense: 5, CritRate: 0.05, MaxHp: 204, MaxMp: 28}, DerivedWorn: &worldv1.DerivedStats{PhysicalAttack: 10},
		}
		h.world.mu.Unlock()
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		w := c.expect("Welcome").Data.(*clientv1.Welcome)
		if len(w.Inventory) != 2 || w.Inventory[1].GetItemId() != leatherCap.GetId() || w.Inventory[1].GetStats().GetMaxHp() != 30 || w.Inventory[0].GetStats() != nil {
			t.Fatalf("welcome inventory %+v; want the potion with no stats, the cap with its roll", w.Inventory)
		}
		if len(w.Equipment) != 1 || w.Equipment[0].GetEquipSlot() != "weapon" || w.Equipment[0].GetItem().GetItemId() != sword.GetId() || w.Equipment[0].GetItem().GetStats().GetPhysicalAttack() != 10 || w.Equipment[0].GetItem().GetTab() != "" {
			t.Fatalf("welcome equipment %+v; want the sword in the weapon slot with its roll", w.Equipment)
		}
		if s := w.GetCharacter().GetSheet(); s.GetPrimary().GetStr() != 12 || s.GetPrimary().GetVit() != 10 || s.GetDerived().GetPhysicalAttack() != 34.5 || s.GetDerived().GetCritRate() != 0.05 || s.GetDerived().GetMaxHp() != 204 || s.GetDerivedWorn().GetPhysicalAttack() != 10 {
			t.Fatalf("welcome sheet %+v", s)
		}
		c.expect("Snapshot")

		part(t, "Equip goes to the world as the sender, answering once it applied", func() {
			c.send(wsconn.Message{ReqID: "e-1", Data: &clientv1.Equip{IdempotencyKey: "k-1", ItemId: leatherCap.GetId()}})
			if r := c.expect("Response"); r.ReqID != "e-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v", r)
			}
			equips, _ := h.world.equipCalls()
			if len(equips) != 1 || equips[0].GetPresence().GetCharacterId() != aylaID.String() || equips[0].GetPresence().GetConnectionId() == "" ||
				equips[0].GetIdempotencyKey() != "k-1" || equips[0].GetItemId() != leatherCap.GetId() || equips[0].GetEquipSlot() != "" {
				t.Fatalf("equips %+v", equips)
			}
		})
		part(t, "the world's InventoryChanged carries the equip slots and rolls, and StatsChanged the sheet", func() {
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 20, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
				Revision: 5, Slots: []*worldv1.Slot{{Tab: "equip", Slot: 2}},
				Equipment: []*worldv1.EquipSlot{{EquipSlot: "hat", Item: &worldv1.Item{Id: leatherCap.GetId(), TemplateId: "leather_cap", Quantity: 1, Stats: &worldv1.ItemStats{PhysicalDefense: 3, MaxHp: 30}}}},
			}}})
			ic := c.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
			if len(ic.Slots) != 1 || ic.Slots[0].Item != nil || len(ic.Equipment) != 1 || ic.Equipment[0].GetEquipSlot() != "hat" ||
				ic.Equipment[0].GetItem().GetItemId() != leatherCap.GetId() || ic.Equipment[0].GetItem().GetStats().GetPhysicalDefense() != 3 || ic.Revision != 5 {
				t.Fatalf("inventory changed %+v", ic)
			}
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 20, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
				Sheet: &worldv1.StatSheet{Derived: &worldv1.DerivedStats{PhysicalDefense: 8, MaxHp: 234}, DerivedWorn: &worldv1.DerivedStats{PhysicalDefense: 3, MaxHp: 30}},
				Hp:    204, MaxHp: 234, Mp: 28, MaxMp: 28,
			}}})
			m := c.expect("StatsChanged")
			sc := m.Data.(*clientv1.StatsChanged)
			if m.Tick != 20 || sc.Hp != 204 || sc.MaxHp != 234 || sc.Mp != 28 || sc.MaxMp != 28 || sc.GetSheet().GetDerived().GetMaxHp() != 234 || sc.GetSheet().GetDerivedWorn().GetPhysicalDefense() != 3 {
				t.Fatalf("stats changed %+v at tick %d", sc, m.Tick)
			}
			// Then the cap comes off into the slot asked for: the item goes back in a tab, the equip slot empties.
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 21, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
				Revision: 6, Slots: []*worldv1.Slot{{Tab: "equip", Slot: 7, Item: &worldv1.Item{Id: leatherCap.GetId(), TemplateId: "leather_cap", Tab: "equip", Slot: 7, Quantity: 1, Stats: &worldv1.ItemStats{PhysicalDefense: 3}}}},
				Equipment: []*worldv1.EquipSlot{{EquipSlot: "hat"}},
			}}})
			ic = c.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
			if len(ic.Equipment) != 1 || ic.Equipment[0].GetEquipSlot() != "hat" || ic.Equipment[0].Item != nil || ic.Slots[0].GetItem().GetStats().GetPhysicalDefense() != 3 {
				t.Fatalf("after unequip %+v; want the hat slot empty and the cap with its roll in the tab", ic)
			}
		})
		part(t, "Unequip carries its equip slot, and the tab slot when one is asked for", func() {
			to := int32(7)
			c.send(wsconn.Message{ReqID: "u-1", Data: &clientv1.Unequip{IdempotencyKey: "k-2", EquipSlot: "hat", ToSlot: &to}})
			c.send(wsconn.Message{ReqID: "u-2", Data: &clientv1.Unequip{IdempotencyKey: "k-3", EquipSlot: "weapon"}})
			for _, req := range []string{"u-1", "u-2"} {
				if r := c.expect("Response"); r.ReqID != req || !r.Data.(*clientv1.Response).GetOk() {
					t.Fatalf("response %+v, want ok to %s", r, req)
				}
			}
			_, unequips := h.world.equipCalls()
			if len(unequips) != 2 || unequips[0].GetIdempotencyKey() != "k-2" || unequips[0].GetEquipSlot() != "hat" || unequips[0].ToSlot == nil || unequips[0].GetToSlot() != 7 ||
				unequips[1].GetEquipSlot() != "weapon" || unequips[1].ToSlot != nil || unequips[1].GetPresence().GetCharacterId() != aylaID.String() {
				t.Fatalf("unequips %+v", unequips)
			}
		})
		part(t, "a refusal answers the item service's reason, or the world's own", func() {
			for _, tt := range []struct {
				err  error
				msg  proto.Message
				code model.Public
			}{
				{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_LEVEL_TOO_LOW), &clientv1.Equip{IdempotencyKey: "k-4", ItemId: leatherCap.GetId()}, model.LevelTooLow},
				{grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_WRONG_EQUIP_SLOT), &clientv1.Equip{IdempotencyKey: "k-5", ItemId: leatherCap.GetId(), EquipSlot: "ring_9"}, model.WrongEquipSlot},
				{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_NOTHING_WORN), &clientv1.Unequip{IdempotencyKey: "k-6", EquipSlot: "cape"}, model.NothingWorn},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_CHARACTER_DEAD), &clientv1.Unequip{IdempotencyKey: "k-7", EquipSlot: "weapon"}, model.CharacterDead},
				{status.Error(codes.Unavailable, "busy"), &clientv1.Equip{IdempotencyKey: "k-8", ItemId: leatherCap.GetId()}, model.Unavailable},
			} {
				time.Sleep(time.Second) // under the inventory limit of 5 a second
				h.world.failEquips(tt.err)
				c.send(wsconn.Message{ReqID: "r-" + tt.code.Name, Data: tt.msg})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.code.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.code.Code) {
					t.Fatalf("response %+v, want %s", r, tt.code.Name)
				}
			}
			h.world.failEquips(nil)
		})
		part(t, "a malformed item ID is BAD_ID without reaching the world", func() {
			before, _ := h.world.equipCalls()
			c.send(wsconn.Message{ReqID: "r-9", Data: &clientv1.Equip{IdempotencyKey: "k-9", ItemId: "nope"}})
			if r := c.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.BadID.Code) {
				t.Fatalf("response %+v", r)
			}
			if after, _ := h.world.equipCalls(); len(after) != len(before) {
				t.Fatal("the world was called")
			}
		})
	})
}

func TestUseItemAnswersOnceTheWorldApplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		potion := kernel.NewItemID().String()

		part(t, "UseItem goes to the world as the sender, with the template the client means", func() {
			c.send(wsconn.Message{ReqID: "p-1", Data: &clientv1.UseItem{IdempotencyKey: "k-1", ItemId: potion, Template: "red_potion"}})
			if r := c.expect("Response"); r.ReqID != "p-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v", r)
			}
			uses := h.world.useCalls()
			if len(uses) != 1 || uses[0].GetPresence().GetCharacterId() != aylaID.String() || uses[0].GetPresence().GetConnectionId() == "" ||
				uses[0].GetIdempotencyKey() != "k-1" || uses[0].GetItemId() != potion || uses[0].GetTemplate() != "red_potion" {
				t.Fatalf("uses %+v", uses)
			}
		})
		part(t, "a refusal answers the world's reason, or the item service's", func() {
			for _, tt := range []struct {
				err  error
				code model.Public
			}{
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_HP_FULL), model.HPFull},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_MP_FULL), model.MPFull},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_NOT_USABLE), model.NotUsable},
				{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_NOT_USABLE), model.NotUsable},
				{grpctest.Error(codes.NotFound, itemv1.Reason_REASON_ITEM_NOT_FOUND), model.ItemNotFound},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_CHARACTER_DEAD), model.CharacterDead},
			} {
				h.world.failUses(tt.err)
				c.send(wsconn.Message{ReqID: "r-" + tt.code.Name, Data: &clientv1.UseItem{IdempotencyKey: "k-" + tt.code.Name, ItemId: potion, Template: "red_potion"}})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.code.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.code.Code) {
					t.Fatalf("response %+v, want %s", r, tt.code.Name)
				}
				time.Sleep(time.Second) // under the potion limit of 5 a second
			}
			h.world.failUses(nil)
		})
		part(t, "a malformed item ID is BAD_ID without reaching the world", func() {
			before := len(h.world.useCalls())
			c.send(wsconn.Message{ReqID: "r-9", Data: &clientv1.UseItem{IdempotencyKey: "k-9", ItemId: "nope", Template: "red_potion"}})
			if r := c.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.BadID.Code) {
				t.Fatalf("response %+v", r)
			}
			if len(h.world.useCalls()) != before {
				t.Fatal("the world was called")
			}
		})
	})
}

func TestAnUnexpectedCommandErrorIsInternalAndLoggedWithItsCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		// The item service sent Internal without a cause; that is all the gateway sees and logs.
		items := &fakeItems{err: fmt.Errorf("discard: %w", errors.New("pq: connection reset"))}
		h := newHarness(t, func(d *deps) {
			d.Items = items.client(t)
			d.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		})
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		c.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k", ItemId: kernel.NewItemID().String(), Quantity: 1}})
		r := c.expect("Response").Data.(*clientv1.Response)
		if r.GetOk() || r.Code != int32(model.Internal.Code) || strings.Contains(r.String(), "connection reset") {
			t.Fatalf("response %+v; the cause must stay out of it", r)
		}
		got := logs.String()
		if !strings.Contains(got, `"msg":"command failed"`) || !strings.Contains(got, "code = Internal") || !strings.Contains(got, `"type":"InventoryDiscard"`) || strings.Contains(got, "connection reset") {
			t.Fatalf("log %s, want the message type and the service's Internal, without the cause the service kept", got)
		}
	})
}

// panickingItems is an item client whose Discard panics inside the gateway, on the connection's command goroutine.
type panickingItems struct{ itemv1.ItemServiceClient }

func (panickingItems) Discard(context.Context, *itemv1.DiscardRequest, ...grpc.CallOption) (*itemv1.DiscardResponse, error) {
	panic("nil map in the gateway")
}

// A panic in a command answers internal, keeps the connection and marks the command's trace as failed.
func TestAPanicInACommandAnswersInternalAndTheConnectionLivesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		h := newHarness(t, func(d *deps) {
			d.Items = panickingItems{(&fakeItems{}).client(t)}
			d.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		})
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		before := len(spans.Ended())
		c.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k", ItemId: kernel.NewItemID().String(), Quantity: 1}})
		m := c.expect("Response")
		synctest.Wait()
		i := slices.IndexFunc(spans.Ended()[before:], func(s sdktrace.ReadOnlySpan) bool { return s.Name() == "ws.InventoryDiscard" })
		if i < 0 {
			t.Fatal("the command kept no trace")
		}
		root := spans.Ended()[before+i]
		if root.Status().Code != otelcodes.Error || root.Status().Description != "panic" {
			t.Fatalf("the command's span ended %+v; want an error", root.Status())
		}
		if attrs := attribute.NewSet(root.Attributes()...); !attrs.HasValue("character_id") {
			t.Fatalf("the span names no character: %v", root.Attributes())
		}
		if r := m.Data.(*clientv1.Response); m.ReqID != "r-1" || r.GetOk() || r.Code != int32(model.Internal.Code) || strings.Contains(r.String(), "nil map") {
			t.Fatalf("response %q %+v; the panic must stay out of it", m.ReqID, r)
		}
		c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 5}})
		c.expect("Pong")
		got := logs.String()
		for _, want := range []string{`"msg":"panic recovered"`, `"flow":"ws"`, `"panic":"nil map in the gateway"`, `"type":"InventoryDiscard"`, `"req_id":"r-1"`, `"conn":`} {
			if !strings.Contains(got, want) {
				t.Fatalf("log lacks %s: %s", want, got)
			}
		}
	})
}

// panicsOnceJoining is a world client whose first Join panics inside the gateway, during a Hello's join.
type panicsOnceJoining struct {
	worldv1.WorldServiceClient
	done *atomic.Bool
}

func (c panicsOnceJoining) Join(ctx context.Context, req *worldv1.JoinRequest, opts ...grpc.CallOption) (*worldv1.JoinResponse, error) {
	if c.done.CompareAndSwap(false, true) {
		panic("nil map in the gateway")
	}
	return c.WorldServiceClient.Join(ctx, req, opts...)
}

// panicsOnceListing is an item client whose first Inventory panics inside the gateway, after a Hello's world join.
type panicsOnceListing struct {
	itemv1.ItemServiceClient
	done *atomic.Bool
}

func (c panicsOnceListing) Inventory(ctx context.Context, req *itemv1.InventoryRequest, opts ...grpc.CallOption) (*itemv1.InventoryResponse, error) {
	if c.done.CompareAndSwap(false, true) {
		panic("nil map in the gateway")
	}
	return c.ItemServiceClient.Inventory(ctx, req, opts...)
}

// A Hello that panics closes its connection as internal and leaves its character free for the next Hello.
func TestAPanickingHelloClosesAndFreesItsCharacter(t *testing.T) {
	for _, tt := range []struct {
		name string
		wrap func(t *testing.T, d *deps)
	}{
		{"in its world join", func(_ *testing.T, d *deps) { d.World = panicsOnceJoining{d.World, &atomic.Bool{}} }},
		{"after its world join", func(t *testing.T, d *deps) {
			d.Items = panicsOnceListing{(&fakeItems{}).client(t), &atomic.Bool{}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, func(d *deps) { tt.wrap(t, d) })
				first := h.dial(t)
				first.hello(tokenAlice, aylaID)
				if m, status := first.read(); status != websocket.StatusInternalError {
					t.Fatalf("got %+v, status %d; want the connection closed as internal", m, status)
				}
				second := h.dial(t)
				second.hello(tokenAlice, aylaID)
				second.expect("Welcome")
				second.expect("Snapshot")
			})
		})
	}
}

// Each command runs in a trace of its own, whose ID the world's calls and log lines carry, kept or not.
func TestEachCommandCarriesItsOwnTraceID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		c.send(wsconn.Message{Data: &clientv1.Attack{}})
		c.send(wsconn.Message{Data: &clientv1.Attack{}})
		ids := waitFor(t, "the attacks", h.world.traceIDList, 3)
		if slices.Contains(ids, trace.TraceID{}.String()) || ids[0] == ids[1] || ids[1] == ids[2] || ids[0] == ids[2] {
			t.Fatalf("the Hello's join and two attacks carried %q", ids)
		}
	})
}

// A command that answers after its calls keeps its trace, with the calls as children; one that only queues keeps none.
func TestAnAnsweredCommandKeepsItsTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		before := len(spans.Ended())
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		synctest.Wait()
		ended := spans.Ended()[before:]
		var root sdktrace.ReadOnlySpan
		for _, s := range ended {
			if s.Name() == "ws.Hello" {
				root = s
			}
		}
		if root == nil || root.Parent().IsValid() || !root.SpanContext().IsSampled() {
			t.Fatalf("no sampled ws.Hello root among %d spans", len(ended))
		}
		if root.SpanKind() != trace.SpanKindServer {
			t.Fatalf("the root's kind is %s; want server, as the player's request", root.SpanKind())
		}
		attrs := attribute.NewSet(root.Attributes()...)
		if v, _ := attrs.Value("character_id"); v.AsString() != aylaID.String() {
			t.Fatalf("the root's character_id is %q", v.AsString())
		}
		if v, _ := attrs.Value("conn"); v.AsString() == "" {
			t.Fatal("the root names no connection")
		}
		children := map[string]bool{}
		for _, s := range ended {
			if s.Parent().SpanID() == root.SpanContext().SpanID() && s.SpanKind() == trace.SpanKindClient {
				children[s.Name()] = true
			}
		}
		for _, call := range []string{"internal.world.v1.WorldService/Join", "internal.item.v1.ItemService/Inventory"} {
			if !children[call] {
				t.Fatalf("the Hello's calls %v lack %s", children, call)
			}
		}
		before, calls := len(spans.Ended()), len(h.world.callList())
		c.send(wsconn.Message{Data: &clientv1.Attack{}})
		waitFor(t, "the attack", h.world.callList, calls+1)
		if got := spans.Ended()[before:]; len(got) != 0 {
			t.Fatalf("an Attack kept %d spans, the first %s", len(got), got[0].Name())
		}
	})
}

// failingPortals is a world whose EnterPortal fails in a way the gateway cannot name, and whose Attack is unreachable.
type failingPortals struct{ worldv1.WorldServiceClient }

func (failingPortals) EnterPortal(context.Context, *worldv1.EnterPortalRequest, ...grpc.CallOption) (*worldv1.EnterPortalResponse, error) {
	return nil, status.Error(codes.Internal, "world bug")
}

func (failingPortals) Attack(context.Context, *worldv1.AttackRequest, ...grpc.CallOption) (*worldv1.AttackResponse, error) {
	return nil, status.Error(codes.Unavailable, "connection refused")
}

func TestAWorldCommandsUnexpectedFailureIsLoggedSampled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		fw := newFakeWorld()
		h := newHarnessOn(t, fw, func(d *deps) {
			d.World = failingPortals{fw.client(t)}
			d.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		})
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		failures := func() int {
			synctest.Wait()
			return strings.Count(logs.String(), `"msg":"command failed"`)
		}
		c.send(wsconn.Message{Data: &clientv1.Attack{}}) // a world restarting is no bug: its stream's warnings tell
		for range 2 {                                    // the portal limit's burst
			c.send(wsconn.Message{Data: &clientv1.EnterPortal{}})
		}
		if n := failures(); n != 1 || !strings.Contains(logs.String(), `"type":"EnterPortal"`) || !strings.Contains(logs.String(), "world bug") {
			t.Fatalf("%d failures logged, want the first of a burst with its cause: %s", n, logs.String())
		}
		time.Sleep(15 * time.Second) // past the log's gap, before a ping this client is not reading to answer
		c.send(wsconn.Message{Data: &clientv1.EnterPortal{}})
		if n := failures(); n != 2 {
			t.Fatalf("%d failures logged, want one more once the burst is long past", n)
		}
	})
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

// Each close logs which side ended it and its code, so a connection dropped with no close frame stands apart from a goodbye or a kick.
func TestAClosedConnectionLogsWhoEndedItAndItsCode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		h := newHarness(t, func(d *deps) { d.Logger = slog.New(slog.NewJSONHandler(logs, nil)) })
		_ = h.dial(t).c.Close(websocket.StatusGoingAway, "tab closed")
		_ = h.dial(t).c.CloseNow()
		bad := h.dial(t)
		bad.sendRaw([]byte{0xff, 0xff})
		if _, status := bad.read(); status != websocket.StatusCode(wsconn.CloseBadMessage) {
			t.Fatalf("closed with %d, want %d", status, wsconn.CloseBadMessage)
		}
		synctest.Wait()
		type closed struct {
			End    string `json:"end"`
			Code   int    `json:"code"`
			Reason string `json:"reason"`
		}
		var got []closed
		for line := range strings.Lines(logs.String()) {
			var e struct {
				Msg string `json:"msg"`
				closed
			}
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
			if e.Msg == "ws closed" {
				got = append(got, e.closed)
			}
		}
		slices.SortFunc(got, func(a, b closed) int { return a.Code - b.Code }) // the three end on their own goroutines
		want := []closed{
			{End: "client", Code: int(websocket.StatusGoingAway), Reason: "tab closed"},
			{End: "dropped", Code: wsconn.CloseDropped},
			{End: "server", Code: wsconn.CloseBadMessage, Reason: "bad message"},
		}
		if !slices.Equal(got, want) {
			t.Fatalf("logged closes %+v, want %+v", got, want)
		}
	})
}

// The handler rejects a malformed message before any use case runs.
func TestAMalformedItemIDNeverReachesTheItemService(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &fakeItems{}
		h := newHarness(t, func(d *deps) { d.Items = items.client(t) })
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		c.send(wsconn.Message{ReqID: "r-2", Data: &clientv1.InventoryDiscard{IdempotencyKey: "k", ItemId: "x", Quantity: 1}})
		if r := c.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.BadID.Code) {
			t.Fatalf("a malformed item ID: %+v", r)
		}
		items.mu.Lock()
		defer items.mu.Unlock()
		if len(items.discards) != 0 {
			t.Fatalf("the item service saw %v", items.discards)
		}
	})
}

func TestEnterPortalIsRoutedAsTheSender(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		c.send(wsconn.Message{Seq: 7, Data: &clientv1.EnterPortal{}})
		waitFor(t, "the portal", h.world.portalList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if len(h.world.portals) != 1 || h.world.portals[0].GetCharacterId() != aylaID.String() {
			t.Fatalf("portals %v", h.world.portals)
		}
	})
}

// social builds the gateway with a fake social service and waits for its first subscription.
func social(t *testing.T, fs *fakeSocial, opts ...func(*deps)) *harness {
	t.Helper()
	h := newHarness(t, append(opts, func(d *deps) { d.Social = fs.client(t) })...)
	select {
	case <-fs.subscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway never subscribed to the social service")
	}
	synctest.Wait() // the subscription's registration round has run with no one on, so a login after it registers once
	return h
}

// A connection the world detached (a portal's transfer failed) joins again, where the player was saved.
func TestADetachedConnectionJoinsAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.world.push(aylaID, &worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Detached{Detached: &worldv1.Detached{}}})
		c.expect("Snapshot")
		if joins := h.world.joins(); len(joins) != 2 || joins[1].GetConnectionId() != joins[0].GetConnectionId() {
			t.Fatalf("joins %v, want the same connection joined again", joins)
		}
	})
}

// Deliveries for a connection the gateway does not have mean a stray presence: the gateway takes it off its map, once.
func TestAPresenceWithoutAConnectionIsTakenOffItsMap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		for range 5 {
			h.world.events <- frameTo(brunoID, "c-ghost", 9, &worldv1.Delta{})
		}
		time.Sleep(time.Second)
		left := waitFor(t, "the ghost's leave", h.world.leaves, 1)
		if p := left[0].GetPresence(); len(left) != 1 || p.GetConnectionId() != "c-ghost" || p.GetCharacterId() != brunoID.String() {
			t.Fatalf("leaves %v, want one for the ghost", left)
		}
	})
}

// A Leave the world was too busy to take is tried again.
func TestALeaveTheWorldRefusedIsTriedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.world.mu.Lock()
		h.world.leaveFails = 1
		h.world.mu.Unlock()
		_ = c.c.Close(websocket.StatusNormalClosure, "bye")
		time.Sleep(time.Second)
		if left := waitFor(t, "the leaves", h.world.leaves, 2); len(left) != 2 {
			t.Fatalf("leaves %v, want the refused one and its retry", left)
		}
	})
}

// A Leave the gateway sends on its own runs in a trace of its own: the refused call and its retry carry one trace ID.
func TestABackgroundLeaveRunsInOneTrace(t *testing.T) {
	for _, tt := range []struct {
		name  string
		leave func(h *harness, c *client)
	}{
		{"a close's", func(_ *harness, c *client) { _ = c.c.Close(websocket.StatusNormalClosure, "bye") }},
		{"a ghost's", func(h *harness, _ *client) { h.world.events <- frameTo(brunoID, "c-ghost", 9, &worldv1.Delta{}) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t)
				c := h.dial(t)
				c.hello(tokenAlice, aylaID)
				c.expect("Snapshot")
				h.world.mu.Lock()
				h.world.leaveFails = 1
				h.world.mu.Unlock()
				tt.leave(h, c)
				time.Sleep(time.Second)
				waitFor(t, "the leaves", h.world.leaves, 2)
				if ids := h.world.leaveTraceIDs(); len(ids) != 2 || ids[0] != ids[1] || ids[0] == (trace.TraceID{}).String() {
					t.Fatalf("the Leave and its retry carried %q; want one trace", ids)
				}
			})
		})
	}
}

// A rejoin runs in a trace of its own, so its failure's log line carries the trace ID its world call did.
func TestAFailedRejoinLogsUnderItsCallsTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		log, err := logger.New(logs, logger.Config{Level: "info", Format: "json"})
		if err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, func(d *deps) { d.Logger = log })
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		h.world.mu.Lock()
		h.world.joinErr = status.Error(codes.Internal, "world bug")
		h.world.mu.Unlock()
		h.world.restart("boot-2")
		_, _ = c.read()
		ids := waitFor(t, "the rejoin", h.world.traceIDList, 2)
		var line struct {
			TraceID string `json:"trace_id"`
		}
		for l := range strings.Lines(logs.String()) {
			if strings.Contains(l, `"msg":"rejoin failed"`) {
				_ = json.Unmarshal([]byte(l), &line)
			}
		}
		if line.TraceID == "" || line.TraceID != ids[1] {
			t.Fatalf("rejoin failed logged trace %q; its Join carried %q", line.TraceID, ids[1])
		}
	})
}

// Registering every player again after social restarts is one trace, which each registration carries.
func TestARegistrationRoundRunsInOneTrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		ayla, bruno := h.dial(t), h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Snapshot")
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")
		synctest.Wait()
		fs.restart("boot-2")
		<-fs.subscribed
		synctest.Wait()
		ids := fs.onlineTraceIDs()
		if len(ids) != 4 || ids[2] != ids[3] || ids[2] == (trace.TraceID{}).String() || ids[2] == ids[0] || ids[2] == ids[1] {
			t.Fatalf("registrations carried %q; want each login's own, then one for the round", ids)
		}
	})
}

// A Hello that fails after its world join took the character from the older connection: as the login never happened, the older one is joined again.
func TestAHelloFailingAfterItsJoinRejoinsTheOlderConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sessions := &fakeSessions{tokens: map[string]model.Principal{tokenAlice: {AccountID: alice, SessionID: sessAlice}}}
		h := newHarness(t, func(d *deps) { d.Sessions = sessions })
		older := h.dial(t)
		older.hello(tokenAlice, aylaID)
		older.expect("Snapshot")
		conn := connectionOf(h.world, aylaID)
		sessions.mu.Lock()
		sessions.rotateErr = errors.New("account service down") // the newer Hello fails at its last step, after its join
		sessions.mu.Unlock()
		newer := h.dial(t)
		newer.hello(tokenAlice, aylaID)
		older.expect("Snapshot") // its rejoin's; a Kicked first would close it
		if last := connectionOf(h.world, aylaID); last != conn {
			t.Fatalf("the last join came through %s, want the older connection %s", last, conn)
		}
	})
}

// A Hello whose world join outlived its deadline may or may not have taken the character: the older connection is joined again either way.
func TestAHelloWhoseJoinTimedOutRejoinsTheOlderConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		older := h.dial(t)
		older.hello(tokenAlice, aylaID)
		older.expect("Snapshot")
		conn := connectionOf(h.world, aylaID)
		late, rejoin := make(chan struct{}), make(chan struct{})
		h.world.mu.Lock()
		h.world.joinGate, h.world.joining = late, make(chan struct{}, 1)
		h.world.mu.Unlock()
		newer := h.dial(t)
		newer.hello(tokenAlice, aylaID)
		<-h.world.joining
		h.world.mu.Lock()
		h.world.joinGate = rejoin // joins from here on wait at their own gate
		h.world.mu.Unlock()
		time.Sleep(2 * time.Second) // past the Hello's deadline, and the rejoin's spread
		close(late)                 // the world finishes the newer join, its reply lost to the gateway
		synctest.Wait()
		close(rejoin)
		older.expect("Snapshot")
		if last := connectionOf(h.world, aylaID); last != conn {
			t.Fatalf("the last join came through %s, want the older connection %s", last, conn)
		}
	})
}

// joinSeqOf is the sequence the fake world gave the character's last join: its place among all joins.
func joinSeqOf(fw *fakeWorld, id kernel.CharacterID) uint64 {
	var seq uint64
	for k, j := range fw.joins() {
		if j.GetCharacterId() == id.String() {
			seq = uint64(k + 1) //nolint:gosec // a test's few joins
		}
	}
	return seq
}

// connectionOf is the connection the character's last world join came through.
func connectionOf(fw *fakeWorld, id kernel.CharacterID) string {
	conn := ""
	for _, j := range fw.joins() {
		if j.GetCharacterId() == id.String() {
			conn = j.GetConnectionId()
		}
	}
	return conn
}

// A party delivery that comes while a Hello is still under way waits for Welcome, as the world's do.
func TestASocialDeliveryDuringHelloFollowsWelcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		items := &fakeItems{gate: make(chan struct{})}
		h := newHarness(t, func(d *deps) { d.Social, d.Items = fs.client(t), items.client(t) })
		<-fs.subscribed
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		synctest.Wait() // joined, waiting for the inventory
		fs.push(aylaID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{InviteId: "inv-1", FromId: brunoID.String(), FromName: "Bruno"}}})
		synctest.Wait()
		close(items.gate)
		if m, _ := c.read(); m.Type != "Welcome" {
			t.Fatalf("first %s, want Welcome", m.Type)
		}
		c.expect("PartyInvited")
	})
}

// An announcement comes once for the whole gateway, which hands it to every player it has in the world.
func TestAnAnnouncementReachesEveryPlayerInTheWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		ayla, bruno := h.dial(t), h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Snapshot")
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")
		news := &socialv1.Announcement{Kind: &socialv1.Announcement_BossAppeared{BossAppeared: &socialv1.BossAppeared{Boss: "king_slime", MapId: "forest"}}}
		fs.events <- &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_Announcement{Announcement: news}}
		for _, c := range []*client{ayla, bruno} {
			if b := c.expect("Announcement").Data.(*clientv1.Announcement).GetBossAppeared(); b.GetBoss() != "king_slime" || b.GetMapId() != "forest" {
				t.Fatalf("announced %+v", b)
			}
		}
	})
}

// Registering players again runs beside the social stream: one that hangs holds up no other player.
func TestAHungRegistrationHoldsUpNobody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		ayla, bruno := h.dial(t), h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Snapshot")
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")
		synctest.Wait()
		fs.set(func(f *fakeSocial) { f.hang = map[string]bool{aylaID.String(): true} })
		fs.restart("boot-2")
		<-fs.subscribed
		synctest.Wait()
		start := time.Now()
		fs.push(brunoID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{InviteId: "inv-1", FromId: aylaID.String(), FromName: "Ayla"}}})
		bruno.expect("PartyInvited")
		if waited := time.Since(start); waited > time.Second {
			t.Fatalf("Bruno's invite waited %v behind Ayla's registration", waited)
		}
		registered := false
		for _, req := range fs.onlineCalls() {
			registered = registered || req.GetMember().GetId() == brunoID.String() && req.GetConnectionId() != ""
		}
		if !registered {
			t.Fatal("Bruno was not registered again")
		}
	})
}

// A login's registration with social runs beside its commands: a social that does not answer holds up none of them, and the gauge counts the login waiting.
func TestASlowSocialHoldsUpNoCommandAfterWelcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		gate := make(chan struct{})
		fs.set(func(f *fakeSocial) { f.gate = gate })
		c := h.joined(t)
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}})
		synctest.Wait()
		if got := h.world.inputCount(); got != 1 {
			t.Fatalf("%d inputs reached the world while social held the registration, want 1", got)
		}
		if n := h.obs.registering(); n != 1 {
			t.Fatalf("%d logins counted waiting to register, want 1", n)
		}
		close(gate)
		synctest.Wait()
		if n := h.obs.registering(); n != 0 {
			t.Fatalf("%d logins counted waiting after social answered, want 0", n)
		}
	})
}

// A registration still on its way when the connection closes cannot land after the close's Offline.
func TestARegistrationRacingTheCloseLeavesThePlayerOffline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		ayla := h.joined(t)
		synctest.Wait()
		gate := make(chan struct{})
		fs.set(func(f *fakeSocial) { f.gate = gate })
		fs.restart("boot-2")
		<-fs.subscribed
		synctest.Wait() // Ayla's registration again waits in the service
		_ = ayla.c.Close(websocket.StatusNormalClosure, "bye")
		synctest.Wait()
		close(gate)
		synctest.Wait()
		if last := fs.lastLanded(aylaID); last != "offline" {
			t.Fatalf("Ayla is %s after her connection closed", last)
		}
	})
}

// A Hello naming another account's character takes none of its player's deliveries while the world checks it.
func TestAHelloForAnotherAccountsCharacterLeavesItsPlayerAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		h := social(t, fs)
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Snapshot")
		gate, joining := make(chan struct{}), make(chan struct{}, 1)
		h.world.mu.Lock()
		h.world.joinGate, h.world.joining = gate, joining
		h.world.mu.Unlock()

		intruder := h.dial(t)
		intruder.hello(tokenBob, aylaID)
		<-joining
		fs.push(aylaID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{InviteId: "inv-1", FromId: brunoID.String(), FromName: "Bruno"}}})
		if inv := ayla.expect("PartyInvited").Data.(*clientv1.PartyInvited); inv.GetInviteId() != "inv-1" {
			t.Fatalf("invited %+v", inv)
		}
		close(gate)
		intruder.expectKick(clientv1.KickReason_KICK_REASON_UNAUTHENTICATED)
		h.world.delta(aylaID, 9, &worldv1.Delta{AckSeq: 7})
		if d := ayla.expect("Delta").Data.(*clientv1.Delta); d.AckSeq != 7 {
			t.Fatalf("delta %+v", d)
		}
	})
}

func TestChatRouting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		fs.recipients = []kernel.CharacterID{aylaID, brunoID}
		h := social(t, fs)
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Snapshot")
		bruno := h.dial(t)
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")

		part(t, "a map line goes to the sender's map with the connection's identity", func() {
			ayla.send(wsconn.Message{Data: &clientv1.ChatSend{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Text: "hello map"}})
			waitFor(t, "the map line", h.world.sayList, 1)
			h.world.mu.Lock()
			defer h.world.mu.Unlock()
			if len(h.world.says) != 1 || h.world.says[0].GetPresence().GetCharacterId() != aylaID.String() || h.world.says[0].GetPresence().GetConnectionId() == "" || h.world.says[0].GetSenderName() != "Ayla" || h.world.says[0].GetText() != "hello map" {
				t.Fatalf("says %+v", h.world.says)
			}
		})
		part(t, "a map line from the world reaches the client as ChatMessage", func() {
			h.world.push(brunoID, &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Chat{Chat: &worldv1.Chat{SenderId: aylaID.String(), SenderName: "Ayla", Text: "hello map"}}})
			cm := bruno.expect("ChatMessage").Data.(*clientv1.ChatMessage)
			if cm.Channel != clientv1.ChatChannel_CHAT_CHANNEL_MAP || cm.Sender != aylaID.String() || cm.SenderName != "Ayla" || cm.Text != "hello map" {
				t.Fatalf("chat message %+v", cm)
			}
		})
		part(t, "a party line the social service delivers reaches every recipient, the sender included", func() {
			ayla.send(wsconn.Message{Data: &clientv1.ChatSend{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "psst"}})
			for _, c := range []*client{ayla, bruno} {
				cm := c.expect("ChatMessage").Data.(*clientv1.ChatMessage)
				if cm.Channel != clientv1.ChatChannel_CHAT_CHANNEL_PARTY || cm.Sender != aylaID.String() || cm.SenderName != "Ayla" || cm.Text != "psst" {
					t.Fatalf("party chat %+v", cm)
				}
			}
		})
		part(t, "a line on no channel is refused", func() {
			ayla.send(wsconn.Message{Data: &clientv1.ChatSend{Text: "where?"}})
			if r := ayla.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.BadChatChannel.Code) {
				t.Fatalf("response %+v", r)
			}
		})
		part(t, "a refused line answers a Response without a request ID", func() {
			fs.mu.Lock()
			fs.chatErr = grpctest.Error(codes.InvalidArgument, socialv1.Reason_REASON_CHAT_TOO_LONG)
			fs.mu.Unlock()
			ayla.send(wsconn.Message{Data: &clientv1.ChatSend{Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Text: "..."}})
			r := ayla.expect("Response")
			if r.ReqID != "" || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(model.ChatTooLong.Code) {
				t.Fatalf("response %+v", r)
			}
		})
	})
}

// A registration that fails at login is tried again in the background, so the player still gets their party.
func TestALoginsFailedRegistrationIsRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		fs.of[aylaID.String()] = &socialv1.Party{LeaderId: aylaID.String(), Members: []*socialv1.Member{{Id: aylaID.String(), Name: "Ayla", Online: true}}}
		h := social(t, fs)
		fs.set(func(f *fakeSocial) { f.downFor = 2 })
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Welcome")
		if p := ayla.expectEach("Snapshot", "PartyState")["PartyState"].Data.(*clientv1.PartyState).GetParty(); p.GetLeader() != aylaID.String() {
			t.Fatalf("party %+v; want hers, once a retry registered her", p)
		}
		if n := len(fs.onlineCalls()); n != 3 {
			t.Fatalf("%d registrations; want two failed and the one that landed", n)
		}
	})
}

func TestPartyCommandsAndState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fs := newFakeSocial()
		fs.of[aylaID.String()] = &socialv1.Party{LeaderId: aylaID.String(), Members: []*socialv1.Member{
			{Id: aylaID.String(), Name: "Ayla", Level: 1, Hp: 204, MaxHp: 204, MapId: "town", Online: true},
			{Id: brunoID.String(), Name: "Bruno", Class: "magician", Level: 1, MapId: "forest"},
		}}
		fs.of[brunoID.String()] = nil
		h := social(t, fs)
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expect("Welcome")
		// The party follows Welcome, from the registration, in order with the party's later changes.
		if p := ayla.expectEach("Snapshot", "PartyState")["PartyState"].Data.(*clientv1.PartyState).GetParty(); p.GetLeader() != aylaID.String() || len(p.GetMembers()) != 2 || p.GetMembers()[1].GetName() != "Bruno" || p.GetMembers()[1].GetClass() != "magician" || p.GetMembers()[1].GetMapId() != "forest" || p.GetMembers()[1].GetOnline() {
			t.Fatalf("ayla's party after Welcome %+v", p)
		}
		bruno := h.dial(t)
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Welcome")
		if p := bruno.expectEach("Snapshot", "PartyState")["PartyState"].Data.(*clientv1.PartyState).GetParty(); p != nil {
			t.Fatalf("bruno has a party: %+v", p)
		}
		// Registrations land in either order, slightly after their Snapshots: wait for both, find Ayla's by ID.
		synctest.Wait()
		if len(fs.onlineCalls()) < 2 {
			t.Fatalf("online %+v", fs.onlineCalls())
		}
		var mine *socialv1.OnlineRequest
		for _, req := range fs.onlineCalls() {
			if req.GetMember().GetId() == aylaID.String() {
				mine = req
			}
		}
		if m := mine.GetMember(); m.GetName() != "Ayla" || m.GetClass() != "warrior" || m.GetMapId() != "town" || m.GetMaxHp() != 204 || m.GetLevel() != 1 || mine.GetGatewayId() == "" || mine.GetConnectionId() != connectionOf(h.world, aylaID) || mine.GetJoinSeq() != joinSeqOf(h.world, aylaID) {
			t.Fatalf("online %+v", fs.onlineCalls())
		}

		part(t, "each command carries the connection's identity and answers its request", func() {
			for k, tt := range []struct {
				msg  any
				want string
			}{
				{&clientv1.PartyInvite{CharacterId: brunoID.String()}, "invite " + aylaID.String() + " " + brunoID.String()},
				{&clientv1.PartyRespond{InviteId: "inv-9", Accept: true}, "respond " + aylaID.String() + " inv-9 yes"},
				{&clientv1.PartyKick{CharacterId: brunoID.String()}, "kick " + aylaID.String() + " " + brunoID.String()},
				{&clientv1.PartyPromote{CharacterId: brunoID.String()}, "promote " + aylaID.String() + " " + brunoID.String()},
				{&clientv1.PartyLeave{}, "leave " + aylaID.String()},
			} {
				req := "p-" + string(rune('a'+k))
				ayla.send(wsconn.Message{ReqID: req, Data: tt.msg})
				r := ayla.expect("Response")
				if r.ReqID != req || !r.Data.(*clientv1.Response).GetOk() {
					t.Fatalf("%T: response %+v", tt.msg, r)
				}
				if got := fs.lastCall(); got != tt.want {
					t.Fatalf("%T: call %q, want %q", tt.msg, got, tt.want)
				}
			}
		})
		part(t, "errors map to the Response code", func() {
			time.Sleep(time.Second) // the five above spent the party budget's burst; two more refill in a second
			ayla.send(wsconn.Message{ReqID: "p-x", Data: &clientv1.PartyInvite{CharacterId: "nope"}})
			if r := ayla.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.BadID.Code) {
				t.Fatalf("malformed id: %+v", r)
			}
			fs.mu.Lock()
			fs.err = grpctest.Error(codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER)
			fs.mu.Unlock()
			ayla.send(wsconn.Message{ReqID: "p-y", Data: &clientv1.PartyKick{CharacterId: brunoID.String()}})
			if r := ayla.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != int32(model.NotPartyLeader.Code) {
				t.Fatalf("not leader: %+v", r)
			}
			fs.mu.Lock()
			fs.err = nil
			fs.mu.Unlock()
		})
		part(t, "PartyState and PartyInvited from the social service's stream reach the connections", func() {
			state := &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{Party: &socialv1.Party{LeaderId: brunoID.String(), Members: []*socialv1.Member{{Id: brunoID.String(), Name: "Bruno", Online: true}, {Id: aylaID.String(), Name: "Ayla", Online: true}}}}}
			for _, id := range []kernel.CharacterID{aylaID, brunoID, robinID} { // robin is not connected here: dropped
				fs.push(id, &socialv1.DeliverResponse{Event: state})
			}
			for _, c := range []*client{ayla, bruno} {
				ps := c.expect("PartyState").Data.(*clientv1.PartyState)
				if ps.Party == nil || ps.GetParty().GetLeader() != brunoID.String() || len(ps.GetParty().GetMembers()) != 2 {
					t.Fatalf("party state %+v", ps)
				}
			}
			fs.push(brunoID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyInvited{PartyInvited: &socialv1.PartyInvited{InviteId: "inv-2", FromId: aylaID.String(), FromName: "Ayla"}}})
			if inv := bruno.expect("PartyInvited").Data.(*clientv1.PartyInvited); inv.GetInviteId() != "inv-2" || inv.From != aylaID.String() || inv.FromName != "Ayla" {
				t.Fatalf("invited %+v", inv)
			}
			fs.push(aylaID, &socialv1.DeliverResponse{Event: &socialv1.DeliverResponse_PartyState{PartyState: &socialv1.PartyState{}}})
			if ps := ayla.expect("PartyState").Data.(*clientv1.PartyState); ps.Party != nil {
				t.Fatalf("party state after leaving %+v", ps)
			}
		})
		part(t, "a social restart clears every party and registers everyone again", func() {
			before := len(fs.onlineCalls())
			fs.restart("boot-2")
			<-fs.subscribed
			for _, c := range []*client{ayla, bruno} {
				if ps := c.expect("PartyState").Data.(*clientv1.PartyState); ps.Party != nil {
					t.Fatalf("party state after the restart %+v", ps)
				}
			}
			synctest.Wait()
			if len(fs.onlineCalls()) < before+2 {
				t.Fatalf("online calls %d, want %d", len(fs.onlineCalls()), before+2)
			}
			ayla.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 9}})
			ayla.expect("Pong") // the socket stayed up
		})
		part(t, "a stream that breaks without a restart registers everyone again, and sends no PartyState", func() {
			// The service answers each Online with the member's party; with none to send, any PartyState is the gateway's own.
			fs.mu.Lock()
			clear(fs.of)
			fs.mu.Unlock()
			before := len(fs.onlineCalls())
			fs.restart("boot-2")
			select {
			case <-fs.subscribed:
			case <-time.After(5 * time.Second):
				t.Fatal("no new subscription")
			}
			synctest.Wait() // the registrations land, so a PartyState they caused would come before the Pong
			ayla.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 10}})
			if m, _ := ayla.read(); m.Type != "Pong" {
				t.Fatalf("got %+v; a PartyState was sent although the parties survived", m)
			}
			synctest.Wait()
			if len(fs.onlineCalls()) < before+2 {
				t.Fatalf("online calls %d, want %d: the social service may have taken them offline", len(fs.onlineCalls()), before+2)
			}
		})
		part(t, "closing the connection tells the social service", func() {
			_ = bruno.c.Close(websocket.StatusNormalClosure, "bye")
			synctest.Wait()
			fs.mu.Lock()
			defer fs.mu.Unlock()
			if len(fs.offline) != 1 || fs.offline[0].GetCharacterId() != brunoID.String() || fs.offline[0].GetConnectionId() != connectionOf(h.world, brunoID) {
				t.Fatalf("Offline calls %v, want one naming Bruno's connection", fs.offline)
			}
		})
	})
}

func TestWelcomeCarriesTheRotatedToken(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		w := c.expect("Welcome").Data.(*clientv1.Welcome)
		if w.Token == "" || w.Token == tokenAlice || !strings.HasPrefix(w.Token, "rotated-") {
			t.Fatalf("welcome token %q", w.Token)
		}
	})
}

func TestSecondHelloOnTheSameSessionKicksTheFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		first := h.dial(t)
		first.hello(tokenAlice, aylaID)
		first.expect("Snapshot")
		second := h.dial(t)
		second.hello(tokenAlice, aylaID) // the same login session, as a retried Hello would be
		second.expect("Snapshot")
		first.expectKick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
	})
}

func TestExpiredSessionIsKicked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sessions := &fakeSessions{tokens: map[string]model.Principal{tokenAlice: {AccountID: alice, SessionID: sessAlice}}}
		h := newHarness(t, func(d *deps) {
			d.Sessions = sessions
			d.SessionCheckInterval = 20 * time.Millisecond
		})
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Snapshot")
		time.Sleep(60 * time.Millisecond) // a few checks pass while the session is fine
		sessions.expire(sessAlice)
		c.expectKick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
	})
}

type watcher struct {
	accountv1.UnimplementedAccountServiceServer
	mu            sync.Mutex
	subscriptions []string      // the gateway IDs that subscribed, in order
	subscribed    chan struct{} // told of a subscription, if it has room
	events        chan *accountv1.WatchSessionsResponse
	end           chan struct{}
}

// newWatcher is an account service whose session stream stays quiet.
func newWatcher(t *testing.T) clients.SessionWatcher {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	accountv1.RegisterAccountServiceServer(srv, &watcher{subscribed: make(chan struct{}, 1), events: make(chan *accountv1.WatchSessionsResponse), end: make(chan struct{})})
	return accountv1.NewAccountServiceClient(grpctest.Conn(t, srv))
}

func (w *watcher) WatchSessions(req *accountv1.WatchSessionsRequest, stream grpc.ServerStreamingServer[accountv1.WatchSessionsResponse]) error {
	w.mu.Lock()
	w.subscriptions = append(w.subscriptions, req.GetGatewayId())
	w.mu.Unlock()
	select {
	case w.subscribed <- struct{}{}:
	default:
	}
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
	synctest.Test(t, func(t *testing.T) {
		w := &watcher{subscribed: make(chan struct{}, 1), events: make(chan *accountv1.WatchSessionsResponse), end: make(chan struct{})}
		srv := grpcx.NewServer(grpcx.ServerOptions{})
		accountv1.RegisterAccountServiceServer(srv, w)
		h := newHarness(t, func(d *deps) {
			d.Watcher = accountv1.NewAccountServiceClient(grpctest.Conn(t, srv))
		})

		part(t, "a new login elsewhere kicks as logged_in_elsewhere", func() {
			c := h.dial(t)
			c.hello(tokenAlice, aylaID)
			c.expect("Snapshot")
			w.events <- &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_AccountRevoked{AccountRevoked: &accountv1.AccountRevoked{
				AccountId: alice.String(), Reason: accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN,
			}}}
			c.expectKick(clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE)
		})
		part(t, "a session ended by reuse kicks as session_expired", func() {
			c := h.dial(t)
			c.hello(tokenBob, brunoID)
			c.expect("Snapshot")
			w.events <- &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_SessionRevoked{SessionRevoked: &accountv1.SessionRevoked{
				SessionId: sessBob.String(), Reason: accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE,
			}}}
			c.expectKick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
		})
		part(t, "logging out everywhere kicks as session_expired; an account with nobody connected changes nothing", func() {
			c := h.dial(t)
			c.hello(tokenAlice, robinID)
			c.expect("Snapshot")
			for _, account := range []kernel.AccountID{kernel.NewAccountID(), alice} {
				w.events <- &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_AccountRevoked{AccountRevoked: &accountv1.AccountRevoked{
					AccountId: account.String(), Reason: accountv1.RevokeReason_REVOKE_REASON_LOGOUT,
				}}}
			}
			c.expectKick(clientv1.KickReason_KICK_REASON_SESSION_EXPIRED)
		})
		part(t, "after the stream breaks the gateway subscribes again, under its ID", func() {
			close(w.end)
			timeout := time.After(5 * time.Second)
			for w.count() < 2 {
				select {
				case <-w.subscribed:
				case <-timeout:
					t.Fatalf("subscriptions %v", w.subscriptions)
				}
			}
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.subscriptions[0] != "gw-test" || w.subscriptions[1] != "gw-test" {
				t.Fatalf("subscriptions %v", w.subscriptions)
			}
		})
	})
}

func TestDeliveriesBeforeWelcomeWaitAndFollowIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		// Before Join answers: an older connection's Delta (not this session's), the Snapshot, a Delta; the rest follow Welcome.
		h.world.before = []*worldv1.DeliverResponse{frameTo(kernel.CharacterID{}, "", 1, &worldv1.Delta{AckSeq: 99})}
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
		h.world.delta(aylaID, 42, &worldv1.Delta{AckSeq: 1})
		if d := c.expect("Delta"); d.Data.(*clientv1.Delta).AckSeq != 1 {
			t.Fatalf("delta %+v: the older presence's came through", d)
		}
	})
}

func TestAfterTheWorldsStreamBreaksEveryPlayerIsJoinedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items := &fakeItems{inventory: []*itemv1.Item{{Id: "i-1", TemplateId: "red_potion", Tab: "use", Quantity: 3}}, revision: 7}
		soc := newFakeSocial()
		soc.of = map[string]*socialv1.Party{aylaID.String(): {LeaderId: aylaID.String(), Members: []*socialv1.Member{{Id: aylaID.String(), Name: "Ayla", Online: true}}}}
		h := social(t, soc, func(d *deps) { d.Items = items.client(t) })
		ayla := h.dial(t)
		ayla.hello(tokenAlice, aylaID)
		ayla.expectEach("Snapshot", "PartyState") // the login's
		bruno := h.dial(t)
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")
		h.world.delta(aylaID, 500, &worldv1.Delta{})
		if d := ayla.expect("Delta"); d.Tick != 500 {
			t.Fatalf("delta tick %d", d.Tick)
		}
		// Bruno's last checkpoint is in the forest; Ayla's is where she was. The restarted world loads the quest logs saved.
		h.world.save(brunoID, "forest", 64, 608)
		h.world.mu.Lock()
		h.world.quests = []*worldv1.QuestState{{QuestId: "slime_cleanup", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{4}}}
		h.world.mu.Unlock()
		items.mu.Lock()
		items.revision = 8 // a pickup's push lost in the break
		items.mu.Unlock()
		h.world.restart("boot-2")

		// Each gets the whole screen again, as the services have it, with no token: pushes in the break may be lost.
		wantQuests := []*clientv1.QuestState{{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{4}}}
		rejoined := func(c *client, mapID string, after uint64, also ...string) map[string]wsconn.Message {
			t.Helper()
			m, status := c.read()
			w, ok := m.Data.(*clientv1.Welcome)
			if status != -1 || !ok || w.GetToken() != "" || w.GetMapId() != mapID || w.GetInventoryRevision() != 8 || len(w.GetInventory()) != 1 ||
				!slices.EqualFunc(w.GetQuests(), wantQuests, func(a, b *clientv1.QuestState) bool { return proto.Equal(a, b) }) {
				t.Fatalf("got %+v (status %d) first, want a Welcome on %s with no token, the inventory and the quests the rejoin found", m, status, mapID)
			}
			got := c.expectEach(append([]string{"Snapshot"}, also...)...)
			if snap := got["Snapshot"]; snap.Tick <= after {
				t.Fatalf("snapshot tick %d after %d: the ticks went back on the connection", snap.Tick, after)
			}
			return got
		}
		aylas := rejoined(ayla, "town", 500, "PartyState")
		rejoined(bruno, "forest", 0) // its Welcome moves it: no MapChange
		if p := aylas["PartyState"].Data.(*clientv1.PartyState).GetParty(); p.GetLeader() != aylaID.String() {
			t.Fatalf("party %v; the Welcome emptied it, and Online sends it again", p)
		}
		if got := h.obs.welcomed(); !slices.Equal(got, []string{"login", "login", "rejoin", "rejoin"}) {
			t.Fatalf("welcomes measured %v", got)
		}
		joins := h.world.joins()
		if len(joins) != 4 || joins[2].GetConnectionId() == "" {
			t.Fatalf("joins %+v", joins)
		}
		for _, c := range []*client{ayla, bruno} {
			c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}})
			c.expect("Pong") // the socket stayed open
		}
		bruno.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}})
		waitFor(t, "Bruno's input", h.world.inputList, 1)
		h.world.mu.Lock()
		defer h.world.mu.Unlock()
		if in := h.world.inputs; len(in) != 1 || in[0].GetPresence().GetConnectionId() != h.world.joined[1].GetConnectionId() {
			t.Fatalf("inputs %+v: commands must carry the rejoined connection", in)
		}
		if online := h.obs.onlineAfter(t, 2); online != 2 {
			t.Fatalf("players online %d; a rejoin is not a new player", online)
		}
	})
}

func TestAHelloWhileTheWorldRestartsWaitsForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.world.restart("boot-2") // the stream is down until the gateway subscribes again
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
	})
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
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.joined(t)
		h.world.mu.Lock()
		h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
		h.world.mu.Unlock()
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Attack{}})
		if err := <-h.world.blocked; err != nil { // the world holds the attack
			t.Fatal(err)
		}
		c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 5}})
		if p := c.expect("Pong").Data.(*clientv1.Pong); p.ClientTime != 5 {
			t.Fatalf("pong %+v", p)
		}
		close(h.world.block)
		if err := <-h.world.blocked; err != nil {
			t.Fatalf("the attack stopped with %v", err)
		}
	})
}

func TestCommandsReachTheServicesInTheOrderTheyArrived(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.joined(t)
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}})
		c.send(wsconn.Message{Seq: 2, Data: &clientv1.Attack{}})
		c.send(wsconn.Message{Seq: 3, Data: &clientv1.Input{}})
		c.send(wsconn.Message{Seq: 4, Data: &clientv1.EnterPortal{}})
		c.send(wsconn.Message{Seq: 5, Data: &clientv1.Input{Left: true}})
		got := waitFor(t, "five commands", h.world.callList, 5)
		want := []string{"input 1", "attack", "input 3", "portal", "input 5"}
		if strings.Join(got, ", ") != strings.Join(want, ", ") {
			t.Fatalf("calls %v, want %v", got, want)
		}
	})
}

func TestAFullCommandQueueAnswersRateLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, func(d *deps) { d.CommandQueue = 2 })
		c := h.joined(t)
		h.world.mu.Lock()
		h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
		h.world.mu.Unlock()
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Attack{}})
		if err := <-h.world.blocked; err != nil { // in flight; the queue behind it is empty
			t.Fatal(err)
		}
		c.send(wsconn.Message{Seq: 2, Data: &clientv1.Input{Right: true}})
		c.send(wsconn.Message{Seq: 3, Data: &clientv1.Input{}})
		c.send(wsconn.Message{Seq: 4, Data: &clientv1.Input{Left: true}}) // dropped: no request ID to answer
		c.send(wsconn.Message{ReqID: "p-1", Data: &clientv1.PartyLeave{}})
		r := c.expect("Response")
		if r.ReqID != "p-1" || r.Data.(*clientv1.Response).Code != int32(model.RateLimited.Code) {
			t.Fatalf("response %+v", r)
		}
		close(h.world.block)
		if err := <-h.world.blocked; err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the queued inputs", h.world.inputList, 2) // and nothing else is coming
		if got := h.world.inputList(); len(got) != 2 || got[0].GetSeq() != 2 || got[1].GetSeq() != 3 {
			t.Fatalf("inputs %+v, want the two that fit", got)
		}
	})
}

// Closing the connection cancels its command in flight, which is no failure to log.
func TestClosingTheConnectionCancelsItsCommandInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := &lockedBuffer{}
		h := newHarness(t, func(d *deps) { d.Logger = slog.New(slog.NewJSONHandler(logs, nil)) })
		c := h.joined(t)
		h.world.mu.Lock()
		h.world.block, h.world.blocked = make(chan struct{}), make(chan error, 2)
		h.world.mu.Unlock()
		c.send(wsconn.Message{Seq: 1, Data: &clientv1.Attack{}})
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
		if strings.Contains(logs.String(), "command failed") {
			t.Fatalf("the cancelled attack was logged as a failure: %s", logs.String())
		}
	})
}

func TestDrainKicksEveryConnectionAsServerRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		ayla := h.joined(t)
		bruno := h.dial(t)
		bruno.hello(tokenBob, brunoID)
		bruno.expect("Snapshot")
		silent := h.dial(t) // connected, no Hello yet
		drained := make(chan struct{})
		go func() { defer close(drained); h.streams.Drain(context.Background()) }()
		for _, c := range []*client{ayla, bruno, silent} {
			c.expectKick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
		}
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Fatal("Drain did not return once every connection closed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, res, err := websocket.Dial(ctx, gatewayURL, &websocket.DialOptions{HTTPClient: h.http})
		if err == nil || res == nil || res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("a new connection while draining: err %v, response %v", err, res)
		}
		if left := waitFor(t, "the leaves", h.world.leaves, 2); len(left) != 2 {
			t.Fatalf("left %+v", left)
		}
	})
}

// Drain returns once each connection's close has taken its player off the map, so no Leave is cut by the exit.
func TestDrainWaitsForEachClosesLeave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		ayla := h.joined(t)
		gate := make(chan struct{})
		h.world.mu.Lock()
		h.world.leaveGate = gate
		h.world.mu.Unlock()
		drained := make(chan struct{})
		go func() { defer close(drained); h.streams.Drain(context.Background()) }()
		ayla.expectKick(clientv1.KickReason_KICK_REASON_SERVER_RESTART)
		time.Sleep(time.Second) // Drain polls while the Leave waits in the world
		select {
		case <-drained:
			t.Fatal("Drain returned while the close's Leave was still on its way")
		default:
		}
		close(gate)
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Fatal("Drain did not return once the Leave was in")
		}
	})
}

// Drain waits too for a ghost's Leave still on its way, so the exit cuts no background job short.
func TestDrainWaitsForAGhostsLeave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		gate := make(chan struct{})
		h.world.mu.Lock()
		h.world.leaveGate = gate
		h.world.mu.Unlock()
		h.world.events <- frameTo(brunoID, "c-ghost", 9, &worldv1.Delta{})
		synctest.Wait()
		drained := make(chan struct{})
		go func() { defer close(drained); h.streams.Drain(context.Background()) }()
		time.Sleep(time.Second)
		select {
		case <-drained:
			t.Fatal("Drain returned while a ghost's Leave was still on its way")
		default:
		}
		close(gate)
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Fatal("Drain did not return once the ghost's Leave was in")
		}
		if left := waitFor(t, "the ghost's leave", h.world.leaves, 1); len(left) != 1 {
			t.Fatalf("leaves %v, want the ghost's", left)
		}
	})
}

func TestTheFirstSubscriptionIsNotABreak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Subscribed arrives while the first join is in flight: one join, one Snapshot.
		fw := newFakeWorld()
		fw.hold = make(chan struct{})
		h := newHarnessOn(t, fw)
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		c.expect("Welcome")
		c.expect("Snapshot")
		c.send(wsconn.Message{Data: &clientv1.Ping{ClientTime: 1}})
		c.expect("Pong")
		time.Sleep(time.Second) // a rejoin, at once or after a backoff, would be in by now
		synctest.Wait()
		if n := len(h.world.joins()); n != 1 {
			t.Fatalf("%d joins: the first subscription was taken for a break", n)
		}
	})
}

// fakeConn is a connection the test drives the handler's callbacks with.
type fakeConn struct{ id string }

func (c fakeConn) ID() string                  { return c.id }
func (fakeConn) Send(wsconn.Message) error     { return nil }
func (fakeConn) Close(code int, reason string) {}

// A push the codec cannot encode is a programming error: its close is logged as one, with the type.
func TestAnEncodeFailureIsLoggedAsAnError(t *testing.T) {
	logs := &lockedBuffer{}
	d := deps{ID: "gw-test", Logger: slog.New(slog.NewJSONHandler(logs, nil)), HelloTimeout: time.Minute, CommandQueue: 4, Traces: traces}
	_, handler := d.build()
	h := handler.(*ws.Handler)
	c := fakeConn{id: "conn-1"}
	state := h.OnOpen(c)
	h.OnClose(c, state, &wsconn.CloseError{Code: wsconn.CloseInternal, Reason: "internal error", Err: fmt.Errorf("%w: Snapshot: invalid UTF-8", wsconn.ErrEncode)})
	got := logs.String()
	for _, want := range []string{`"level":"ERROR"`, `"conn":"conn-1"`, `Snapshot: invalid UTF-8`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log lacks %s: %s", want, got)
		}
	}
}

func TestPointsCommandsAnswerOnceTheWorldApplies(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.world.mu.Lock()
		h.world.chars[aylaID].view.Sheet = &worldv1.StatSheet{
			Primary: &worldv1.Stats{Strength: 12}, Points: &worldv1.Points{Unspent: 5, Spent: &worldv1.Stats{}, FreeResetLeft: true}, AttackLengthTicks: 12,
			Derived:       &worldv1.DerivedStats{PhysicalAttack: 20.5, MagicAttack: 8.5, MagicDefense: 10, HpRegen: 0.5, AttackSpeed: 0.016, MoveSpeed: 0.007},
			DerivedSkills: &worldv1.DerivedStats{HpRegen: 0.6}, PrimarySkills: &worldv1.Stats{},
			Skills: &worldv1.SkillPoints{Unspent: 4, Invested: map[string]int32{"second_wind": 1}},
		}
		h.world.mu.Unlock()
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		s := c.expect("Welcome").Data.(*clientv1.Welcome).GetCharacter().GetSheet()
		if sk := s.GetSkills(); sk.GetUnspent() != 4 || sk.GetInvested()["second_wind"] != 1 || s.GetDerivedSkills().GetHpRegen() != 0.6 {
			t.Fatalf("welcome sheet %+v; want the skill points and the skills' share", s)
		}
		if p := s.GetPoints(); p.GetUnspent() != 5 || !p.GetFreeResetLeft() || s.GetAttackLengthTicks() != 12 ||
			s.GetDerived().GetMagicAttack() != 8.5 || s.GetDerived().GetMagicDefense() != 10 || s.GetDerived().GetHpRegen() != 0.5 || s.GetDerived().GetMoveSpeed() != 0.007 {
			t.Fatalf("welcome sheet %+v; want the points, the cooldown and the new derived stats", s)
		}
		c.expect("Snapshot")

		part(t, "SpendPoints and ResetPoints go to the world as the sender, answering once they applied", func() {
			c.send(wsconn.Message{ReqID: "p-1", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 3, Vit: 2}}})
			c.send(wsconn.Message{ReqID: "p-2", Data: &clientv1.ResetPoints{ExpectedUnspent: 0}})
			for _, req := range []string{"p-1", "p-2"} {
				if r := c.expect("Response"); r.ReqID != req || !r.Data.(*clientv1.Response).GetOk() {
					t.Fatalf("response %+v, want ok to %s", r, req)
				}
			}
			spends, resets := h.world.pointsCalls()
			if len(spends) != 1 || spends[0].GetPresence().GetCharacterId() != aylaID.String() || spends[0].GetPresence().GetConnectionId() == "" ||
				spends[0].GetExpectedUnspent() != 5 || spends[0].GetPoints().GetStrength() != 3 || spends[0].GetPoints().GetVitality() != 2 {
				t.Fatalf("spends %+v", spends)
			}
			if len(resets) != 1 || resets[0].GetExpectedUnspent() != 0 || resets[0].GetPresence().GetCharacterId() != aylaID.String() {
				t.Fatalf("resets %+v", resets)
			}
		})
		part(t, "SpendSkillPoints goes to the world as the sender, answering once it applied", func() {
			time.Sleep(time.Second) // under the points limit of 2 a second
			c.send(wsconn.Message{ReqID: "s-1", Data: &clientv1.SpendSkillPoints{ExpectedUnspent: 4, Points: map[string]int32{"strike": 3, "spark": 1}}})
			if r := c.expect("Response"); r.ReqID != "s-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v, want ok to s-1", r)
			}
			invests := h.world.skillPointsCalls()
			if len(invests) != 1 || invests[0].GetPresence().GetCharacterId() != aylaID.String() || invests[0].GetExpectedUnspent() != 4 ||
				invests[0].GetPoints()["strike"] != 3 || invests[0].GetPoints()["spark"] != 1 {
				t.Fatalf("skill point spends %+v", invests)
			}
		})
		part(t, "a refusal answers the world's reason", func() {
			for _, tt := range []struct {
				reason worldv1.Reason
				code   codes.Code
				msg    proto.Message
				want   model.Public
			}{
				{worldv1.Reason_REASON_POINTS_CHANGED, codes.FailedPrecondition, &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 1}}, model.PointsChanged},
				{worldv1.Reason_REASON_NOT_ENOUGH_POINTS, codes.FailedPrecondition, &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 9}}, model.NotEnoughPoints},
				{worldv1.Reason_REASON_BAD_POINTS, codes.InvalidArgument, &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: -1}}, model.BadPoints},
				{worldv1.Reason_REASON_NO_FREE_RESET, codes.FailedPrecondition, &clientv1.ResetPoints{ExpectedUnspent: 5}, model.NoFreeReset},
				{worldv1.Reason_REASON_SKILL_POINTS_CHANGED, codes.FailedPrecondition, &clientv1.SpendSkillPoints{ExpectedUnspent: 4, Points: map[string]int32{"strike": 1}}, model.SkillPointsChanged},
				{worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS, codes.FailedPrecondition, &clientv1.SpendSkillPoints{ExpectedUnspent: 4, Points: map[string]int32{"strike": 9}}, model.NotEnoughSkillPoints},
				{worldv1.Reason_REASON_SKILL_NOT_LEARNABLE, codes.FailedPrecondition, &clientv1.SpendSkillPoints{ExpectedUnspent: 4, Points: map[string]int32{"fireball": 1}}, model.SkillNotLearnable},
				{worldv1.Reason_REASON_BAD_SKILL_POINTS, codes.InvalidArgument, &clientv1.SpendSkillPoints{ExpectedUnspent: 4, Points: map[string]int32{"strike": -1}}, model.BadSkillPoints},
			} {
				time.Sleep(time.Second) // under the points limit of 2 a second
				h.world.failPoints(grpctest.Error(tt.code, tt.reason))
				c.send(wsconn.Message{ReqID: "r-" + tt.want.Name, Data: tt.msg})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.want.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.want.Code) {
					t.Fatalf("response %+v, want %s", r, tt.want.Name)
				}
			}
			h.world.failPoints(nil)
		})
		part(t, "UseSkill goes to the world as the sender, answering once the tick resolved it, or with its refusal", func() {
			c.send(wsconn.Message{ReqID: "u-1", Data: &clientv1.UseSkill{Skill: "strike"}})
			if r := c.expect("Response"); r.ReqID != "u-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v, want ok to u-1", r)
			}
			if casts := h.world.castCalls(); len(casts) != 1 || casts[0].GetPresence().GetCharacterId() != aylaID.String() || casts[0].GetSkill() != "strike" {
				t.Fatalf("skill uses %+v", casts)
			}
			for _, tt := range []struct {
				reason worldv1.Reason
				want   model.Public
			}{
				{worldv1.Reason_REASON_SKILL_NOT_LEARNED, model.SkillNotLearned},
				{worldv1.Reason_REASON_SKILL_PASSIVE, model.SkillPassive},
				{worldv1.Reason_REASON_NOT_IN_TOWN, model.NotInTown},
				{worldv1.Reason_REASON_SKILL_COOLING_DOWN, model.SkillCoolingDown},
				{worldv1.Reason_REASON_NOT_ENOUGH_MP, model.NotEnoughMP},
				{worldv1.Reason_REASON_SKILL_WAITING, model.SkillWaiting},
			} {
				time.Sleep(time.Second) // under the skill limit of 5 a second
				h.world.failCasts(grpctest.Error(codes.FailedPrecondition, tt.reason))
				c.send(wsconn.Message{ReqID: "r-" + tt.want.Name, Data: &clientv1.UseSkill{Skill: "strike"}})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.want.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.want.Code) {
					t.Fatalf("response %+v, want %s", r, tt.want.Name)
				}
			}
			h.world.failCasts(nil)
		})
		part(t, "Promote goes to the world as the sender, answering once applied, or with its refusal", func() {
			c.send(wsconn.Message{ReqID: "p-1", Data: &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}})
			if r := c.expect("Response"); r.ReqID != "p-1" || !r.Data.(*clientv1.Response).GetOk() {
				t.Fatalf("response %+v, want ok to p-1", r)
			}
			if got := h.world.promoteCalls(); len(got) != 1 || got[0].GetPresence().GetCharacterId() != aylaID.String() || got[0].GetNpcId() != "warrior_trainer" || got[0].GetClass() != "warrior" {
				t.Fatalf("promotes %+v", got)
			}
			for _, tt := range []struct {
				reason worldv1.Reason
				want   model.Public
			}{
				{worldv1.Reason_REASON_PROMOTION_CONDITIONS_UNMET, model.PromotionUnmet},
				{worldv1.Reason_REASON_NOT_YOUR_NEXT_CLASS, model.NotYourNextClass},
				{worldv1.Reason_REASON_NPC_OUT_OF_REACH, model.NpcOutOfReach},
			} {
				time.Sleep(time.Second) // under the dialog limit
				h.world.failPromotes(grpctest.Error(codes.FailedPrecondition, tt.reason))
				c.send(wsconn.Message{ReqID: "r-" + tt.want.Name, Data: &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.want.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.want.Code) {
					t.Fatalf("response %+v, want %s", r, tt.want.Name)
				}
			}
			h.world.failPromotes(nil)
		})
	})
}

// The Welcome carries the skills' timers the world joined the character with, as left on its last checkpoint; a kind from a newer world is left out.
func TestAWelcomeCarriesTheSkillTimersTheWorldJoinedWith(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.world.mu.Lock()
		h.world.timers = []*worldv1.SkillTimer{
			{SkillId: "iron_body", Kind: worldv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: 40_000},
			{SkillId: "iron_body", Kind: worldv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: 20_000, Points: 2},
			{SkillId: "odd", Kind: worldv1.SkillTimerKind(9), LeftMs: 1},
		}
		h.world.mu.Unlock()
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		want := []*clientv1.SkillTimer{
			{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, LeftMs: 40_000},
			{Skill: "iron_body", Kind: clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, LeftMs: 20_000, Points: 2},
		}
		if got := c.expect("Welcome").Data.(*clientv1.Welcome).GetSkillTimers(); !slices.EqualFunc(got, want, func(a, b *clientv1.SkillTimer) bool { return proto.Equal(a, b) }) {
			t.Fatalf("welcome skill timers %v, want %v", got, want)
		}
	})
}

func TestQuestCommandsAndEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.world.mu.Lock()
		h.world.quests = []*worldv1.QuestState{
			{QuestId: "slime_cleanup", Status: worldv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: worldv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
		}
		h.world.mu.Unlock()
		c := h.dial(t)
		c.hello(tokenAlice, aylaID)
		want := []*clientv1.QuestState{
			{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
		}
		if got := c.expect("Welcome").Data.(*clientv1.Welcome).GetQuests(); !slices.EqualFunc(got, want, func(a, b *clientv1.QuestState) bool { return proto.Equal(a, b) }) {
			t.Fatalf("welcome quests %v, want %v", got, want)
		}
		c.expect("Snapshot")

		part(t, "the four commands go to the world as the sender, answering once it applied them", func() {
			c.send(wsconn.Message{ReqID: "q-1", Data: &clientv1.Talk{NpcId: "town_guide"}})
			c.send(wsconn.Message{ReqID: "q-2", Data: &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}})
			c.send(wsconn.Message{ReqID: "q-3", Data: &clientv1.AbandonQuest{QuestId: "slime_cleanup"}})
			c.send(wsconn.Message{ReqID: "q-4", Data: &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}})
			for _, req := range []string{"q-1", "q-2", "q-3", "q-4"} {
				if r := c.expect("Response"); r.ReqID != req || !r.Data.(*clientv1.Response).GetOk() {
					t.Fatalf("response %+v, want ok to %s", r, req)
				}
			}
			calls := h.world.questList()
			wantCalls := []proto.Message{
				&worldv1.TalkRequest{NpcId: "town_guide"},
				&worldv1.AcceptQuestRequest{NpcId: "town_guide", QuestId: "slime_cleanup"},
				&worldv1.AbandonQuestRequest{QuestId: "slime_cleanup"},
				&worldv1.HandInQuestRequest{NpcId: "town_guide", QuestId: "sticky_samples"},
			}
			if len(calls) != len(wantCalls) {
				t.Fatalf("world calls %v, want %v", calls, wantCalls)
			}
			for i, call := range calls {
				p := call.ProtoReflect()
				presence := p.Get(p.Descriptor().Fields().ByName("presence")).Message().Interface().(*worldv1.Presence)
				if presence.GetCharacterId() != aylaID.String() || presence.GetConnectionId() == "" {
					t.Fatalf("call %d %v isn't Ayla's through her connection", i, call)
				}
				p.Clear(p.Descriptor().Fields().ByName("presence"))
				if !proto.Equal(call, wantCalls[i]) {
					t.Fatalf("call %d %v, want %v", i, call, wantCalls[i])
				}
			}
		})
		part(t, "the dialog and a changed quest reach the client", func() {
			h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 20, Event: &worldv1.DeliverResponse_NpcDialog{NpcDialog: &worldv1.NpcDialog{
				NpcId: "town_guide",
				Gives: []*worldv1.QuestOffer{
					{QuestId: "slime_cleanup", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED, Unmet: &worldv1.QuestConditions{}},
					{QuestId: "sticky_samples", State: worldv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &worldv1.QuestConditions{Level: 2, Class: "warrior", Quests: []string{"slime_cleanup"}}},
				},
				Takes: []*worldv1.QuestTurnIn{{QuestId: "sticky_samples", Ready: true}},
			}}})
			wantDialog := &clientv1.NpcDialog{
				NpcId: "town_guide",
				Gives: []*clientv1.QuestOffer{
					{QuestId: "slime_cleanup", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
					{QuestId: "sticky_samples", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Class: "warrior", Quests: []string{"slime_cleanup"}}},
				},
				Takes: []*clientv1.QuestTurnIn{{QuestId: "sticky_samples", Ready: true}},
			}
			if d := c.expect("NpcDialog"); !proto.Equal(d.Data.(*clientv1.NpcDialog), wantDialog) {
				t.Fatalf("dialog %v, want %v", d.Data, wantDialog)
			}
			for _, tt := range []struct {
				change worldv1.QuestChange
				status worldv1.QuestStatus
				want   *clientv1.QuestChanged
			}{
				{worldv1.QuestChange_QUEST_CHANGE_PROGRESSED, worldv1.QuestStatus_QUEST_STATUS_ACTIVE, &clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_PROGRESSED, Quest: &clientv1.QuestState{QuestId: "q", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{3, 1}}}},
				{worldv1.QuestChange_QUEST_CHANGE_ACCEPTED, worldv1.QuestStatus_QUEST_STATUS_ACTIVE, &clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_ACCEPTED, Quest: &clientv1.QuestState{QuestId: "q", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{3, 1}}}},
				{worldv1.QuestChange_QUEST_CHANGE_HANDED_IN, worldv1.QuestStatus_QUEST_STATUS_FINISHED, &clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_HANDED_IN, Quest: &clientv1.QuestState{QuestId: "q", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, Progress: []int32{3, 1}}}},
				{worldv1.QuestChange_QUEST_CHANGE_ABANDONED, worldv1.QuestStatus_QUEST_STATUS_UNSPECIFIED, &clientv1.QuestChanged{Change: clientv1.QuestChange_QUEST_CHANGE_ABANDONED, Quest: &clientv1.QuestState{QuestId: "q", Progress: []int32{3, 1}}}},
			} {
				h.world.push(aylaID, &worldv1.DeliverResponse{Tick: 21, Event: &worldv1.DeliverResponse_QuestChanged{QuestChanged: &worldv1.QuestChanged{
					Change: tt.change, Quest: &worldv1.QuestState{QuestId: "q", Status: tt.status, Progress: []int32{3, 1}},
				}}})
				if m := c.expect("QuestChanged"); m.Tick != 21 || !proto.Equal(m.Data.(*clientv1.QuestChanged), tt.want) {
					t.Fatalf("quest changed %v at %d, want %v", m.Data, m.Tick, tt.want)
				}
			}
		})
		part(t, "a refusal answers the world's reason, and the item service's for a hand-in", func() {
			for _, tt := range []struct {
				err  error
				msg  proto.Message
				want model.Public
			}{
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_NPC_OUT_OF_REACH), &clientv1.Talk{NpcId: "town_guide"}, model.NpcOutOfReach},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_UNAVAILABLE), &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "nope"}, model.QuestUnavailable},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_CONDITIONS_UNMET), &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.QuestConditionsUnmet},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE), &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.QuestAlreadyActive},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_ALREADY_FINISHED), &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}, model.QuestAlreadyFinished},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_TOO_MANY_QUESTS), &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.TooManyQuests},
				{grpctest.Error(codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_NOT_READY), &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.QuestNotReady},
				{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_NOT_ENOUGH_ITEMS), &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.NotEnoughItems},
				{grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_USE_TAB_FULL), &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}, model.UseTabFull},
			} {
				time.Sleep(time.Second) // under the dialog limit of 5 a second
				h.world.failQuests(tt.err)
				c.send(wsconn.Message{ReqID: "r-" + tt.want.Name, Data: tt.msg})
				if r := c.expect("Response"); r.ReqID != "r-"+tt.want.Name || r.Data.(*clientv1.Response).GetOk() || r.Data.(*clientv1.Response).Code != int32(tt.want.Code) {
					t.Fatalf("response %+v, want %s", r, tt.want.Name)
				}
			}
			h.world.failQuests(nil)
		})
		part(t, "the dialog limit allows five at once and refuses the sixth", func() {
			time.Sleep(time.Second)
			for i := range 6 {
				c.send(wsconn.Message{ReqID: fmt.Sprintf("t-%d", i), Data: &clientv1.Talk{NpcId: "town_guide"}})
			}
			// The refused one is answered at once on the read loop, ahead of the queue: match by request ID.
			answers := map[string]int32{} // 0 is ok
			for range 6 {
				m := c.expect("Response")
				answers[m.ReqID] = m.Data.(*clientv1.Response).GetCode()
			}
			if answers["t-5"] != int32(model.RateLimited.Code) || answers["t-0"] != 0 || answers["t-4"] != 0 || len(answers) != 6 {
				t.Fatalf("answers %v", answers)
			}
		})
	})
}
