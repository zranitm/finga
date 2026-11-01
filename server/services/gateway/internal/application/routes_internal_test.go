package application

import (
	"log/slog"
	"slices"
	"testing"

	"finga/internal/protocol"
)

// Every message a client may send has a route, and nothing else does: a
// message added to the protocol without one fails here rather than
// answering not_implemented in a game.
func TestEveryClientMessageHasARoute(t *testing.T) {
	g := NewGateway(Dependencies{Logger: slog.New(slog.DiscardHandler)})
	if got, want := g.router.Types(), protocol.ClientMessages(); !slices.Equal(got, want) {
		t.Fatalf("routed  %v\nprotocol %v", got, want)
	}
}
