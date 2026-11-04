package receive

import (
	"log/slog"
	"slices"
	"testing"

	"finga/internal/protocol"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
)

// A protocol message without a route fails here rather than answering not_implemented in a game.
func TestEveryClientMessageHasARoute(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	h := New(send.New(send.Deps{Logger: log}, session.NewRegistry(session.Deps{Logger: log})), Options{})
	if got, want := h.router.Types(), protocol.ClientMessages(); !slices.Equal(got, want) {
		t.Fatalf("routed  %v\nprotocol %v", got, want)
	}
}
