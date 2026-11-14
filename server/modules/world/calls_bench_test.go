package world

import (
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// inputs is a map that takes every input at once.
type inputs struct{ service }

func (inputs) Input(kernel.CharacterID, string, uint32, sim.Input, uint32) error { return nil }

// One held key through the facade's call: its span, counter and timer, unsampled as an Input is in production, and sampled into a recorder.
func BenchmarkAnInputThroughTheFacade(b *testing.B) {
	for name, sampler := range map[string]sdktrace.Sampler{"unsampled": sdktrace.NeverSample(), "sampled": sdktrace.AlwaysSample()} {
		b.Run(name, func(b *testing.B) {
			calls, err := facade.New(facade.Options{
				Module: "world", Registry: prometheus.NewRegistry(), Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Errors: errorTable,
				Traces: sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler), sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder())),
			})
			if err != nil {
				b.Fatal(err)
			}
			m := &Module{calls: calls, svc: inputs{}, deliver: hub.New[Delivery](deliverQueue)}
			p, in := Presence{Character: kernel.NewCharacterID(), Conn: "c-a"}, sim.Input{Right: true}
			b.ReportAllocs()
			for b.Loop() {
				if err := m.Input(b.Context(), p, 7, in, 3); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
