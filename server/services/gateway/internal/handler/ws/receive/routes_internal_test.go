package receive

import (
	"log/slog"
	"slices"
	"testing"

	"finga/internal/protocol"
	"finga/services/gateway/internal/application"
)

// A protocol message without a route fails here rather than answering not_implemented in a game.
func TestEveryClientMessageHasARoute(t *testing.T) {
	h := New(application.NewGateway(application.Dependencies{Logger: slog.New(slog.DiscardHandler)}), Options{})
	if got, want := h.router.Types(), protocol.ClientMessages(); !slices.Equal(got, want) {
		t.Fatalf("routed  %v\nprotocol %v", got, want)
	}
}
