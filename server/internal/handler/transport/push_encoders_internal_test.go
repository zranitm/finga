package transport

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	model "finga/internal/model/account"
	"finga/pkg/wsconn"
)

// A push has one encoder: a second registration is an error at startup, not whichever ran last.
func TestASecondEncoderForAPushIsRefused(t *testing.T) {
	e := NewEncoders()
	if err := Encode(e, func(model.MapChange) any { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Encode(e, func(model.MapChange) any { return nil }); err == nil {
		t.Fatal("a second encoder for MapChange was accepted")
	}
	if err := Encode(e, func(model.Died) any { return nil }); err != nil {
		t.Fatalf("an encoder for another push was refused: %v", err)
	}
}

// A server with no section of the Welcome could not answer a Hello, so it does not start.
func TestNoWelcomeSectionIsRefused(t *testing.T) {
	if err := NewEncoders().Complete(); err == nil {
		t.Fatal("encoders with no Welcome section were complete")
	}
}

// watched is a connection and an observer that keep what the outbox did.
type watched struct {
	mu        sync.Mutex
	sent      int
	unencoded []string
}

func (*watched) ID() string                  { return "c-1" }
func (*watched) Close(int, string)           {}
func (w *watched) Send(wsconn.Message) error { w.mu.Lock(); w.sent++; w.mu.Unlock(); return nil }
func (*watched) MessageIn(string)            {}
func (*watched) MessageOut(string)           {}
func (*watched) WelcomeSent(string, int)     {}
func (*watched) SlowConsumer()               {}
func (w *watched) Unencoded(pushType string) {
	w.mu.Lock()
	w.unencoded = append(w.unencoded, pushType)
	w.mu.Unlock()
}

// A push no handler registered an encoder for is a bug: nothing is sent for it, and it is counted and logged with its type, never dropped silently.
func TestAPushWithoutAnEncoderIsCountedAndLogged(t *testing.T) {
	w := &watched{}
	logs := &bytes.Buffer{}
	NewOutbox(w, w, slog.New(slog.NewJSONHandler(logs, nil)), NewEncoders()).Deliver(model.Delivery{Tick: 9, Push: model.MapChange{MapID: "town"}})
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sent != 0 || len(w.unencoded) != 1 || w.unencoded[0] != "MapChange" {
		t.Fatalf("sent %d, counted %v; want nothing sent and MapChange counted once", w.sent, w.unencoded)
	}
	if got := logs.String(); !strings.Contains(got, `"level":"ERROR"`) || !strings.Contains(got, `"push":"MapChange"`) {
		t.Fatalf("logged %q, want an error naming the push's type", got)
	}
}
