package world

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/application/world/itemcalls"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// An instance asked to open before the World runs, and shut before it does, never ticks, and a join waiting in its inbox ends with ErrStopped; the World then ends without a failure, and opens nothing after it stops.
func TestAnInstanceShutBeforeStartNeverRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := gamedatatest.Shipped(t)
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
		w := New(Deps{
			NewRoll: func() model.Roll { return half{} }, Observer: quiet{}, Parties: alone{}, Items: nothing{}, Batches: nothing{}, ItemCalls: itemcalls.NewPool(itemcalls.Deps{Observer: quiet{}, Panics: rec}, itemcalls.Options{Workers: 1}), Traces: noop.NewTracerProvider(),
			Data: data, Saver: nothing{}, Characters: nothing{}, Starter: nothing{}, SaveObserver: nothing{}, Channels: &told{}, Panics: rec, Logger: slog.New(slog.DiscardHandler),
		}, Tuning{TickInterval: 50 * time.Millisecond, CheckpointInterval: time.Hour, FlushTimeout: time.Second, BatchMax: 32, ChannelCap: 1, ChannelIdle: time.Minute})
		place, _ := data.Map("greyfang_cave")
		c, err := w.openChannel(place, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := w.opened.get(c.id); !ok {
			t.Fatal("an instance asked to open before start isn't there to join")
		}
		joined := make(chan error, 1)
		go func() { joined <- c.inst.Join(context.Background(), model.Player{ID: kernel.NewCharacterID()}, nil) }()
		synctest.Wait()
		select {
		case err := <-joined:
			t.Fatalf("joined, or refused, before the instance ran or was shut: %v", err)
		default:
		}
		w.opened.shut(c.id)
		synctest.Wait()
		if _, ok := w.opened.get(c.id); ok {
			t.Fatal("a shut instance is still there")
		}
		if err := <-joined; !errors.Is(err, model.ErrStopped) {
			t.Fatalf("the waiting join got %v; want ErrStopped", err)
		}
		ctx, cancel := context.WithCancelCause(context.Background())
		w.opened.start(ctx, cancel)
		if err := w.opened.stop(); err != nil {
			t.Fatalf("the World ended with %v; want nothing run", err)
		}
		if _, err := w.openChannel(place, 2); !errors.Is(err, model.ErrStopped) {
			t.Fatalf("a channel opened after the World stopped: %v", err)
		}
	})
}
