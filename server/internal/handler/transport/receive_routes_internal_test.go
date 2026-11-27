package transport

import (
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	clientv1 "finga/api/gen/client/v1"
)

// A message has one route: a second registration is an error at startup, not whichever ran last.
func TestASecondRouteForAMessageIsRefused(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	route := func(r *Receiver) {
		Handle(r, "Input", func(*Context, *clientv1.Input) (any, error) { return nil, nil })
	}
	opts := ReceiveOptions{Logger: log, Traces: noop.NewTracerProvider()}
	if _, err := NewReceiver(nil, opts, route); err != nil {
		t.Fatalf("one route for Input: %v", err)
	}
	if _, err := NewReceiver(nil, opts, route, route); err == nil {
		t.Fatal("two routes for Input were accepted")
	}
	if _, err := NewReceiver(nil, opts, func(r *Receiver) {
		Handle(r, "Ping", func(*Context, *clientv1.Ping) (any, error) { return nil, nil })
	}); err == nil {
		t.Fatal("a module's route for Ping, which the transport answers itself, was accepted")
	}
}
