package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/panics"
	"finga/services/world/internal/application"
)

// panicsOn is a tick observer that panics on one map's next tick once armed.
type panicsOn struct {
	mapID string
	armed atomic.Bool
}

func (p *panicsOn) ObserveTick(mapID string, _, _ time.Duration) {
	if mapID == p.mapID && p.armed.Load() {
		panic("corrupt " + mapID)
	}
}

func TestAPanickingMapStopsTheWorldAndTheOtherMapsSave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{}
		obs := &panicsOn{mapID: "cave"}
		w := application.New(data, saver, slog.New(slog.DiscardHandler), application.Options{TickInterval: tickInterval, CheckpointInterval: 30 * time.Second, Observer: obs})
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(context.Background()) }()

		forest, _ := w.Instance("forest")
		fm, _ := data.Map("forest")
		if err := forest.Join(context.Background(), newPlayer("Robin", fm.PlayerSpawn.X, fm.PlayerSpawn.Y), &sink{}); err != nil {
			t.Fatal(err)
		}
		obs.armed.Store(true)

		err := <-runDone
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "corrupt cave" || !strings.Contains(err.Error(), "map cave") {
			t.Fatalf("Run returned %v", err)
		}
		if got := saver.attemptsSoFar(); len(got) != 1 || got[0] != "cp forest" {
			t.Fatalf("saves %v, want the forest player's checkpoint", got)
		}
	})
}

// economy counts the worker pool's jobs.
type economy struct{ started, finished atomic.Int32 }

func (e *economy) JobStarted()  { e.started.Add(1) }
func (e *economy) JobFinished() { e.finished.Add(1) }

func TestAPanickingJobIsRecordedAndTheWorkerGoesOn(t *testing.T) {
	var logs bytes.Buffer
	obs := &economy{}
	w := application.NewWorkers(1, obs, panics.NewRecorder(slog.New(slog.NewJSONHandler(&logs, nil)), nil))
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

func (finishPanics) JobStarted()  {}
func (finishPanics) JobFinished() { panic("broken gauge") }

func TestAPanicInAWorkerOutsideAJobStopsThePool(t *testing.T) {
	w := application.NewWorkers(2, finishPanics{}, nil)
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
