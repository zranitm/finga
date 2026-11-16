package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// server is a game server on a socket for one bot: after its Hello, answer writes what the world sends, and every message the bot sends goes to sent till it hangs up.
type server struct {
	url    string
	sent   chan wsconn.Message
	hungUp chan struct{}
}

func fakeWorld(t *testing.T, answer func(ctx context.Context, write func(wsconn.Message))) *server {
	t.Helper()
	w := &server{sent: make(chan wsconn.Message, 1024), hungUp: make(chan struct{})}
	codec := protocol.ServerCodec(wsconn.Binary)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(rw, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.SubprotocolProto}})
		if err != nil {
			return
		}
		defer close(w.hungUp)
		ctx := r.Context()
		if _, _, err := conn.Read(ctx); err != nil { // its Hello
			return
		}
		go answer(ctx, func(m wsconn.Message) {
			data, _ := codec.Encode(m)
			_ = conn.Write(ctx, websocket.MessageBinary, data)
		})
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if m, err := codec.Decode(data); err == nil {
				w.sent <- m
			}
		}
	}))
	t.Cleanup(srv.Close)
	w.url = srv.URL
	return w
}

// A bot sends an Input each tick, as the client does, each with the next seq and the newest frame as its view; an attack follows its tick's Input and names it, and a chat line names none.
func TestABotSendsAnInputEachTickAndItsAttacksNameTheLatest(t *testing.T) {
	data, wd := testWorld(t)
	w := fakeWorld(t, func(_ context.Context, write func(wsconn.Message)) {
		write(wsconn.Message{Data: &clientv1.Welcome{Token: "t", MapId: data.StartTown().ID, TickRateHz: 20}})
		write(wsconn.Message{Tick: 7, Data: &clientv1.Snapshot{}})
	})
	b := newBot(w.url, "bot", "Bot", 0)
	b.data, b.world, b.future, b.start = data, wd, class(t, data, "warrior"), time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 600*time.Millisecond)
	defer cancel()
	if err := b.enter(ctx, "token"); err != nil {
		t.Fatal(err)
	}
	b.dead = true                       // each decision is then quick, under -race and coverage too: this is the sending, not the walking
	b.command(&clientv1.Attack{Aim: 1}) // decided before the first tick's Input goes
	b.command(&clientv1.ChatSend{Text: "hi"})
	b.loop(ctx, []*bot{b})
	b.hangUp(true)
	<-w.hungUp
	close(w.sent)
	var inputs []wsconn.Message
	var attack, chat *wsconn.Message
	attackAfter := 0 // the Inputs sent before the attack
	for m := range w.sent {
		switch m.Data.(type) {
		case *clientv1.Input:
			inputs = append(inputs, m)
		case *clientv1.Attack:
			attack, attackAfter = &m, len(inputs)
		case *clientv1.ChatSend:
			chat = &m
		}
	}
	if len(inputs) < 9 || len(inputs) > 13 {
		t.Fatalf("%d inputs in 600 ms of 50 ms ticks; want one a tick", len(inputs))
	}
	for i, m := range inputs {
		if m.Seq != uint32(i+1) || m.Data.(*clientv1.Input).GetView() != 7 { //nolint:gosec // a handful of inputs
			t.Fatalf("input %d: seq %d, view %d; want seq %d naming frame 7", i, m.Seq, m.Data.(*clientv1.Input).GetView(), i+1)
		}
	}
	if attack == nil || attackAfter != 1 || attack.Seq != 1 || chat == nil || chat.Seq != 0 {
		t.Fatalf("attack %+v after %d inputs, chat %+v; want the attack right after the first tick's input, naming it, the chat naming none", attack, attackAfter, chat)
	}
}

// An enter that never gets its Snapshot hangs up, and its reader stops though the world went on sending past what the bot would hold.
func TestAFailedEnterHangsUp(t *testing.T) {
	w := fakeWorld(t, func(ctx context.Context, write func(wsconn.Message)) {
		for range 5000 {
			if ctx.Err() != nil {
				return
			}
			write(wsconn.Message{Data: &clientv1.Pong{}})
		}
	})
	b := newBot(w.url, "bot", "Bot", 0)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if err := b.enter(ctx, "token"); err == nil {
		t.Fatal("entered with no Snapshot")
	}
	for name, ch := range map[string]chan struct{}{"the world to see it hang up": w.hungUp, "its reader to stop": b.closed} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("waited 2 s for %s", name)
		}
	}
}
