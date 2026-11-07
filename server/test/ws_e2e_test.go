package e2e_test

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// player is a REST session plus a binary Protobuf WebSocket, read in the background since a timed-out Read closes it.
type player struct {
	t     *testing.T
	id    string
	token string // the session token after Hello rotated it
	conn  *websocket.Conn
	codec *wsconn.ProtoCodec
	msgs  chan wsconn.Message
	done  chan struct{} // closed when the reader stops (connection closed)
}

func (s *stack) enter(t *testing.T, username, charName, class string) *player {
	t.Helper()
	s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	token := s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
	created := s.call("POST", "/api/characters", token, map[string]string{"name": charName, "class": class})
	s.mustStatus(created, http.StatusCreated)
	id := created.body["id"].(string)
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: id}})
	w := p.expect("Welcome").Data.(*clientv1.Welcome)
	if w.GetCharacter().GetName() != charName {
		t.Fatalf("welcome %+v", w)
	}
	p.token = w.Token // rotated on Hello; REST calls use this one from now on
	p.expect("Snapshot")
	return p
}

// connect opens the WebSocket for a character without saying Hello.
func (s *stack) connect(t *testing.T, id string) *player {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A same-page browser Origin, so the gateway's origin check runs as it does for players.
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.url, "http")+"/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {s.url}}, Subprotocols: []string{protocol.SubprotocolProto}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	p := &player{t: t, id: id, conn: conn, codec: protocol.ClientCodec(wsconn.Binary), msgs: make(chan wsconn.Message, 1024), done: make(chan struct{})}
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

func (p *player) send(m wsconn.Message) {
	p.t.Helper()
	b, err := p.codec.Encode(m)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := p.conn.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		p.t.Fatal(err)
	}
}

func (p *player) read(d time.Duration) (wsconn.Message, bool) {
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

func (p *player) closed(d time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(d):
		return false
	}
}

func (p *player) expect(msgType string) wsconn.Message {
	p.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m, ok := p.read(time.Until(deadline))
		if !ok {
			break
		}
		if m.Type == msgType {
			return m
		}
	}
	p.t.Fatalf("no %s within 5 s", msgType)
	return wsconn.Message{}
}

func (p *player) position(of string, d time.Duration) (*clientv1.PlayerView, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		m, ok := p.read(time.Until(deadline))
		if !ok {
			return nil, false
		}
		if m.Type != "Delta" {
			continue
		}
		for _, v := range m.Data.(*clientv1.Delta).Players {
			if v.GetId() == of {
				return v, true
			}
		}
	}
	return nil, false
}

func (p *player) drain(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, ok := p.read(time.Until(deadline)); !ok {
			return
		}
	}
}

func TestWalkTogether(t *testing.T) {
	s := newStack(t)
	ayla := s.enter(t, "alice", "Ayla", "warrior")
	robin := s.enter(t, "bob", "Robin", "bowman")

	t.Run("M1/both-join-the-town-and-see-each-other", func(t *testing.T) {
		// Robin's snapshot already carried Ayla; Ayla learns of Robin from a delta.
		if _, ok := ayla.position(robin.id, 3*time.Second); !ok {
			t.Fatal("Ayla never saw Robin join")
		}
	})

	t.Run("each-sees-the-other-move", func(t *testing.T) {
		ayla.drain(300 * time.Millisecond) // land and settle
		robin.drain(300 * time.Millisecond)
		ayla.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}})
		v1, ok := robin.position(ayla.id, 3*time.Second)
		if !ok {
			t.Fatal("Robin never saw Ayla move")
		}
		v2, ok := robin.position(ayla.id, 3*time.Second)
		if !ok || v2.X <= v1.X || v2.GetVx() != 200 || v2.Facing != 1 {
			t.Fatalf("Ayla as Robin sees her: %+v then %+v", v1, v2)
		}
		ayla.send(wsconn.Message{Seq: 2, Data: &clientv1.Input{}})
		robin.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Left: true}})
		r1, ok := ayla.position(robin.id, 3*time.Second)
		if !ok || r1.GetVx() != -200 || r1.Facing != -1 {
			t.Fatalf("Robin as Ayla sees him: %+v", r1)
		}
		robin.send(wsconn.Message{Seq: 2, Data: &clientv1.Input{}})
	})

	t.Run("faster-inputs-do-not-move-faster", func(t *testing.T) {
		ayla.drain(400 * time.Millisecond)
		robin.drain(400 * time.Millisecond)
		// For 20 ticks Ayla sends one input a tick and Robin ten; both cover the same distance, bounded by walk speed.
		seqA, seqR := uint32(10), uint32(10)
		var startA, startR *clientv1.PlayerView
		for i := range 20 {
			seqA++
			ayla.send(wsconn.Message{Seq: seqA, Data: &clientv1.Input{Right: true}})
			for range 10 {
				seqR++
				robin.send(wsconn.Message{Seq: seqR, Data: &clientv1.Input{Right: true}})
			}
			if i == 0 {
				var ok bool
				if startA, ok = robin.position(ayla.id, 3*time.Second); !ok {
					t.Fatal("no start position for Ayla")
				}
				if startR, ok = ayla.position(robin.id, 3*time.Second); !ok {
					t.Fatal("no start position for Robin")
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		seqA++
		ayla.send(wsconn.Message{Seq: seqA, Data: &clientv1.Input{}})
		seqR++
		robin.send(wsconn.Message{Seq: seqR, Data: &clientv1.Input{}})
		// The stop is each state's last change, so the last delta carrying a character is its end position.
		var endA, endR *clientv1.PlayerView
		for {
			v, ok := robin.position(ayla.id, 500*time.Millisecond)
			if !ok {
				break
			}
			endA = v
		}
		for {
			v, ok := ayla.position(robin.id, 500*time.Millisecond)
			if !ok {
				break
			}
			endR = v
		}
		dA, dR := endA.GetX()-startA.GetX(), endR.GetX()-startR.GetX()
		if dA <= 100 || dR <= 100 {
			t.Fatalf("walked too little: ayla %.0f px, robin %.0f px", dA, dR)
		}
		if math.Abs(dA-dR) > 60 { // a tick or two of jitter is fine; ten times the inputs is not
			t.Fatalf("ten inputs per tick moved Robin %.0f px against Ayla's %.0f px", dR, dA)
		}
		if dA > 260 || dR > 260 { // 20 ticks at 200 px/s is 200 px
			t.Fatalf("faster than walk speed: ayla %.0f px, robin %.0f px in about a second", dA, dR)
		}
	})

	t.Run("M1/leaving-is-seen", func(t *testing.T) {
		_ = robin.conn.Close(websocket.StatusNormalClosure, "bye")
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			m, ok := ayla.read(time.Until(deadline))
			if !ok {
				t.Fatal("Ayla's connection closed")
			}
			if m.Type == "Delta" {
				if slices.Contains(m.Data.(*clientv1.Delta).RemovedPlayers, robin.id) {
					return
				}
			}
		}
		t.Fatal("Ayla never saw Robin leave")
	})
}

func TestSecondLoginKicksTheFirstEndToEnd(t *testing.T) {
	s := newStack(t)
	first := s.enter(t, "carol", "Cass", "warrior")
	token := s.call("POST", "/api/sessions", "", map[string]string{"username": "carol", "password": "correct horse"}).body["token"].(string)
	second := s.connect(t, first.id)
	second.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: first.id}})
	second.expect("Snapshot")
	m := first.expect("Kicked")
	if m.Data.(*clientv1.Kicked).Reason != clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE {
		t.Fatalf("kicked %+v", m.Data)
	}
	if !first.closed(3 * time.Second) {
		t.Fatal("first connection still open after Kicked")
	}
	// The old token is revoked too (one active session per account).
	s.mustStatus(s.call("GET", "/api/characters", first.id, nil), http.StatusUnauthorized)
}
