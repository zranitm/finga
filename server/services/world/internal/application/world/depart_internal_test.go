package world

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/model"
)

type half struct{}

func (half) Float64() float64 { return 0.5 }

type quiet struct{}

func (quiet) ObserveTick(string, time.Duration, time.Duration) {}
func (quiet) JobStarted()                                      {}
func (quiet) JobFinished()                                     {}

// A Leave whose caller stopped waiting still lands; its tombstone goes with it, so the registry holds only players on a map.
func TestATombstoneGoesOnceItsLeaveLands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, err := gamedata.Load("../../../../../data")
		if err != nil {
			t.Fatal(err)
		}
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
		w := New(Deps{
			NewRoll: func() model.Roll { return half{} }, Observer: quiet{}, Workers: economy.NewWorkers(economy.Deps{Observer: quiet{}, Panics: rec}, economy.Options{Workers: 1}), Traces: noop.NewTracerProvider(),
			Data: data, Panics: rec, Logger: slog.New(slog.DiscardHandler),
		}, Tuning{TickInterval: 50 * time.Millisecond, CheckpointInterval: time.Hour, FlushTimeout: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()

		id := kernel.NewCharacterID()
		w.presences.place(id, presence{inst: w.instances[data.Town().ID], conn: "c-1"})
		hurried, stop := context.WithTimeout(context.Background(), time.Millisecond) // gives up before the next tick
		defer stop()
		if err := w.Leave(hurried, id, "c-1"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("leave: %v", err)
		}
		time.Sleep(3 * 50 * time.Millisecond)
		synctest.Wait()
		w.presences.mu.Lock()
		defer w.presences.mu.Unlock()
		if p, ok := w.presences.by[id]; ok {
			t.Fatalf("the registry still holds %+v", p)
		}
	})
}
