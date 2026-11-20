package e2etest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// Player is a REST session plus a binary Protobuf WebSocket, read in the background since a timed-out Read closes it.
type Player struct {
	t     *testing.T
	ID    string
	Token string // the session token after Hello rotated it
	Conn  *websocket.Conn
	codec *wsconn.ProtoCodec
	msgs  chan wsconn.Message
	done  chan struct{}    // closed when the reader stops (connection closed)
	held  []wsconn.Message // read past by hello, for the next read
	// seq is the last Seq sent: inputs and actions on one connection count up together, as the world drops an input no newer than the last it applied.
	Seq         uint32
	attackTicks uint64 // the basic attack's length in the world's ticks, from the Welcome
	TickHz      uint32 // the world's ticks a second, from the Welcome
	tick        uint64 // the newest frame read, Delta or Snapshot, in its map's ticks
	elapsed     uint64 // the world's ticks the frames read have stepped through, across maps
	moved       bool   // a MapChange was read: the next frame is on the new map's clock
	Joined      uint64 // the tick of the Snapshot it joined to, in its map's ticks
	MapID       string // the map it's on, from the Welcome and each MapChange read
	stack       *Stack // whose game data a walk paths on
}

// NextSeq numbers the next input or action this connection sends.
func (p *Player) NextSeq() uint32 {
	p.Seq++
	return p.Seq
}

// Enter signs username up, creates their character and connects it, checking the Welcome names it.
func (s *Stack) Enter(t *testing.T, username, charName, class string) *Player {
	t.Helper()
	s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	token := s.Call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).Body["token"].(string)
	id := s.newCharacter(t, token, charName, class)
	p := s.Connect(t, id)
	if w, _ := p.Hello(token); w.GetCharacter().GetName() != charName {
		t.Fatalf("welcome %+v", w)
	}
	return p
}

// Hello says Hello, then waits for the Welcome, the Snapshot and the login's PartyState, in either order. The PartyState comes from the social registration, so once it has, an invite finds the player on their map.
func (p *Player) Hello(token string) (*clientv1.Welcome, *clientv1.Snapshot) {
	p.t.Helper()
	p.Send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: p.ID}})
	w := p.Expect("Welcome").Data.(*clientv1.Welcome)
	p.Token = w.Token // rotated on Hello; REST calls use this one from now on
	p.attackTicks, p.TickHz, p.MapID = uint64(w.GetCharacter().GetSheet().GetAttackLengthTicks()), w.GetTickRateHz(), w.GetMapId()
	var snap *clientv1.Snapshot
	registered := false
	deadline := time.Now().Add(5 * time.Second)
	for snap == nil || !registered {
		m, ok := p.receive(time.Until(deadline))
		if !ok || time.Now().After(deadline) {
			p.t.Fatalf("%s: within 5 s of the Welcome, a Snapshot %t and the registration's PartyState %t", p.ID, snap != nil, registered)
		}
		switch d := m.Data.(type) {
		case *clientv1.Snapshot:
			if snap == nil {
				snap, p.Joined, p.tick = d, m.Tick, m.Tick // a wait's first frame counts from the join, not from the map's start
				continue
			}
		case *clientv1.PartyState:
			if !registered {
				registered = true
				continue
			}
		}
		if snap != nil {
			p.held = append(p.held, m)
		}
	}
	return w, snap
}

// Connect opens the WebSocket for a character without saying Hello.
func (s *Stack) Connect(t *testing.T, id string) *Player {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A same-page browser Origin, so the gateway's origin check runs as it does for players.
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.URL, "http")+"/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {s.URL}}, Subprotocols: []string{protocol.SubprotocolProto}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	p := &Player{t: t, ID: id, Conn: conn, codec: protocol.ClientCodec(wsconn.Binary), msgs: make(chan wsconn.Message, 1024), done: make(chan struct{}), stack: s}
	go func() {
		defer close(p.done)
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			m, err := p.codec.Decode(data)
			if err != nil {
				return
			}
			p.msgs <- m
		}
	}()
	return p
}

// Send encodes m and writes it to the connection.
func (p *Player) Send(m wsconn.Message) {
	p.t.Helper()
	b, err := p.codec.Encode(m)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := p.Conn.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		p.t.Fatal(err)
	}
}

// Read gives what hello held first, then the next message off the connection.
func (p *Player) Read(d time.Duration) (wsconn.Message, bool) {
	m, ok := wsconn.Message{}, true
	if len(p.held) > 0 {
		m, p.held = p.held[0], p.held[1:]
	} else {
		m, ok = p.receive(d)
	}
	switch d := m.Data.(type) {
	case *clientv1.MapChange:
		p.MapID, p.moved = d.GetMapId(), true
	case *clientv1.Delta, *clientv1.Snapshot:
		p.step(m.Tick)
	}
	return m, ok
}

// receive is the next message off the connection, waiting up to d; with no d, only one already come.
func (p *Player) receive(d time.Duration) (wsconn.Message, bool) {
	if d <= 0 {
		select {
		case m := <-p.msgs:
			return m, true
		default:
			return wsconn.Message{}, false
		}
	}
	select {
	case m := <-p.msgs:
		return m, true
	case <-p.done:
		select {
		case m := <-p.msgs:
			return m, true
		default:
			return wsconn.Message{}, false
		}
	case <-time.After(d):
		return wsconn.Message{}, false
	}
}

// Closed is whether the connection closes within d.
func (p *Player) Closed(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Expect waits for a message of msgType; 15 s only guards a hang, as a slow runner's ticks lag the wall clock.
func (p *Player) Expect(msgType string) wsconn.Message {
	p.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m, ok := p.Read(time.Until(deadline))
		if !ok {
			break
		}
		if m.Type == msgType {
			return m
		}
	}
	p.t.Fatalf("no %s within 15 s", msgType)
	return wsconn.Message{}
}

// Position is the next view of the player of that a Delta brings, within d.
func (p *Player) Position(of string, d time.Duration) (*clientv1.PlayerView, bool) {
	v, _, ok := p.PositionAt(of, d)
	return v, ok
}

// PositionAt is position, with the tick of the frame that carried it.
func (p *Player) PositionAt(of string, d time.Duration) (*clientv1.PlayerView, uint64, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		m, ok := p.Read(time.Until(deadline))
		if !ok {
			return nil, 0, false
		}
		if m.Type != "Delta" {
			continue
		}
		for _, v := range m.Data.(*clientv1.Delta).Players {
			if v.GetId() == of {
				return v, m.Tick, true
			}
		}
	}
	return nil, 0, false
}

// Moving is the first view of of that walks, past any from before, such as a landing still queued on a busy machine.
func (p *Player) Moving(of string, d time.Duration) (*clientv1.PlayerView, bool) {
	deadline := time.Now().Add(d)
	for {
		v, ok := p.Position(of, time.Until(deadline))
		if !ok || v.GetVx() != 0 {
			return v, ok
		}
	}
}

// Drain reads and drops what comes for d.
func (p *Player) Drain(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, ok := p.Read(time.Until(deadline)); !ok {
			return
		}
	}
}
