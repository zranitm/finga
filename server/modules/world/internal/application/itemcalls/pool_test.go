package itemcalls_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"finga/modules/world/internal/application/itemcalls"
	"finga/modules/world/worldtest"
	"finga/pkg/panics"
)

// counts tallies the calls the pool queued and finished.
type counts struct{ started, finished atomic.Int32 }

func (e *counts) CallQueued() { e.started.Add(1) }

func (e *counts) CallDone() { e.finished.Add(1) }

func TestAPanickingJobIsRecordedAndTheWorkerGoesOn(t *testing.T) {
	var logs bytes.Buffer
	obs := &counts{}
	w := itemcalls.NewPool(itemcalls.Deps{Observer: obs, Panics: panics.NewRecorder(slog.New(slog.NewJSONHandler(&logs, nil)), nil)}, itemcalls.Options{Workers: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	ran := make(chan struct{})
	w.Submit(func(context.Context) { panic("bad pickup") })
	w.Submit(func(context.Context) { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker stopped after a job panicked")
	}
	cancel()
	<-done
	if obs.finished.Load() != 2 || !strings.Contains(logs.String(), `"flow":"job"`) || !strings.Contains(logs.String(), `"panic":"bad pickup"`) {
		t.Fatalf("finished %d, log %q", obs.finished.Load(), logs.String())
	}
}

// finishPanics is an observer whose CallDone panics, outside any call.
type finishPanics struct{}

func (finishPanics) CallQueued() {}

func (finishPanics) CallDone() { panic("broken gauge") }

func TestAPanicInAWorkerOutsideAJobStopsThePool(t *testing.T) {
	w := itemcalls.NewPool(itemcalls.Deps{Observer: finishPanics{}, Panics: worldtest.DiscardPanics()}, itemcalls.Options{Workers: 2})
	w.Submit(func(context.Context) {})
	done := make(chan error, 1)
	go func() { done <- w.Run(context.Background()) }()
	select {
	case err := <-done:
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "broken gauge" {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pool kept running after a worker panicked")
	}
}
