package world

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/gamedata/load"
	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/model"
)

type half struct{}

func (half) Float64() float64 { return 0.5 }

type quiet struct{}

func (quiet) ObserveTick(string, time.Duration, time.Duration) {}
func (quiet) CallQueued()                                      {}
func (quiet) CallDone()                                        {}

// nothing stands in for the services this test never reaches: the item, character and save paths.
type nothing struct{}

func (nothing) AddMany(context.Context, model.GrantCall) (model.Granted, error) {
	return model.Granted{}, errNever
}
func (nothing) Retryable(error) bool                                   { return false }
func (nothing) Refused(error) bool                                     { return false }
func (nothing) ObserveBatch(int)                                       {}
func (nothing) SaveCheckpoint(context.Context, model.Checkpoint) error { return errNever }
func (nothing) Load(context.Context, kernel.CharacterID, kernel.AccountID) (model.Character, error) {
	return model.Character{}, errNever
}
func (nothing) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID) error {
	return errNever
}
func (nothing) SaveDropped(string) {}

var errNever = errors.New("not reached in this test")

// alone is a party service where everyone is in no party.
type alone struct{}

func (alone) Party(kernel.CharacterID) model.Party { return model.Party{} }
func (alone) Update(model.MemberUpdate)            {}
func (alone) Epoch() uint64                        { return 0 }

// A Leave whose caller stopped waiting still lands, and the registry forgets the presence, so it holds only players on a map.
func TestALeaveItsCallerGaveUpOnStillClearsTheRegistry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, err := load.Dir("../../../../../data")
		if err != nil {
			t.Fatal(err)
		}
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
		w := New(Deps{
			NewRoll: func() model.Roll { return half{} }, Observer: quiet{}, Parties: alone{}, Items: nothing{}, Batches: nothing{}, ItemCalls: itemcalls.NewPool(itemcalls.Deps{Observer: quiet{}, Panics: rec}, itemcalls.Options{Workers: 1}), Traces: noop.NewTracerProvider(),
			Data: data, Saver: nothing{}, Characters: nothing{}, Starter: nothing{}, SaveObserver: nothing{}, Panics: rec, Logger: slog.New(slog.DiscardHandler),
		}, Tuning{TickInterval: 50 * time.Millisecond, CheckpointInterval: time.Hour, FlushTimeout: time.Second, BatchMax: 32})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()

		id := kernel.NewCharacterID()
		w.presences.place(id, presence{inst: w.instances[data.StartTown().ID], conn: "c-1"})
		hurriedCtx, stop := context.WithTimeout(context.Background(), time.Millisecond) // gives up before the next tick
		defer stop()
		if err := w.Leave(hurriedCtx, id, "c-1"); !errors.Is(err, context.DeadlineExceeded) {
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
