package economy_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"finga/pkg/panics"
	"finga/services/world/internal/application/economy"
	"finga/services/world/worldtest"
)

// counts tallies the jobs the pool started and finished.
type counts struct{ started, finished atomic.Int32 }

func (e *counts) JobStarted() { e.started.Add(1) }

func (e *counts) JobFinished() { e.finished.Add(1) }

func TestAPanickingJobIsRecordedAndTheWorkerGoesOn(t *testing.T) {
	var logs bytes.Buffer
	obs := &counts{}
	w := economy.NewWorkers(1, obs, panics.NewRecorder(slog.New(slog.NewJSONHandler(&logs, nil)), nil))
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

// finishPanics is an economy observer whose JobFinished panics, outside any job.
type finishPanics struct{}

func (finishPanics) JobStarted() {}

func (finishPanics) JobFinished() { panic("broken gauge") }

func TestAPanicInAWorkerOutsideAJobStopsThePool(t *testing.T) {
	w := economy.NewWorkers(2, finishPanics{}, worldtest.DiscardPanics())
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
