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

func (nothing) Add(context.Context, model.Pickup) (model.Inventory, error) {
	return model.Inventory{}, errNever
}
func (nothing) Retryable(error) bool { return false }
func (nothing) Granted(error) bool   { return false }
func (nothing) Refused(error) bool   { return false }
func (nothing) Inventory(context.Context, kernel.CharacterID) (model.Inventory, error) {
	return model.Inventory{}, errNever
}
func (nothing) SaveCheckpoint(context.Context, model.Checkpoint) error { return errNever }
func (nothing) Load(context.Context, kernel.CharacterID, kernel.AccountID) (model.Character, error) {
	return model.Character{}, errNever
}
func (nothing) GrantStarter(context.Context, kernel.AccountID, kernel.CharacterID, kernel.Class) error {
	return errNever
}
func (nothing) SaveDropped(string) {}

var errNever = errors.New("not reached in this test")

// alone is a party service where everyone is in no party.
type alone struct{}

func (alone) Members(kernel.CharacterID) []kernel.CharacterID  { return nil }
func (alone) SameParty(_, _ kernel.CharacterID) bool           { return false }
func (alone) Update(kernel.CharacterID, string, int, int, int) {}
func (alone) Epoch() uint64                                    { return 0 }

// A Leave whose caller stopped waiting still lands, and the registry forgets the presence, so it holds only players on a map.
func TestALeaveItsCallerGaveUpOnStillClearsTheRegistry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, err := gamedata.Load("../../../../../data")
		if err != nil {
			t.Fatal(err)
		}
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
		w := New(Deps{
			NewRoll: func() model.Roll { return half{} }, Observer: quiet{}, Parties: alone{}, Items: nothing{}, ItemCalls: itemcalls.NewPool(itemcalls.Deps{Observer: quiet{}, Panics: rec}, itemcalls.Options{Workers: 1}), Traces: noop.NewTracerProvider(),
			Data: data, Saver: nothing{}, Characters: nothing{}, Starter: nothing{}, SaveObserver: nothing{}, Panics: rec, Logger: slog.New(slog.DiscardHandler),
		}, Tuning{TickInterval: 50 * time.Millisecond, CheckpointInterval: time.Hour, FlushTimeout: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()

		id := kernel.NewCharacterID()
		w.presences.place(id, presence{inst: w.instances[data.Town().ID], conn: "c-1"})
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
