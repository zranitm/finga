package world_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	"finga/pkg/panics"
)

// panicsOn is a tick observer that panics on one map's next tick once armed.
type panicsOn struct {
	mapID string
	armed atomic.Bool
}

func (p *panicsOn) ObserveTick(mapID string, _ int, _, _ time.Duration) {
	if mapID == p.mapID && p.armed.Load() {
		panic("corrupt " + mapID)
	}
}

func TestAPanickingMapStopsTheWorldAndTheOtherMapsSave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{}
		obs := &panicsOn{mapID: "icefall"}
		w := newWorld(t, saver, worldtest.Options{Observer: obs})
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(context.Background()) }()

		w.enter(t, at(t, "Robin", "dune_road"), "dune_road", "c-1", &worldtest.Sink{})
		obs.armed.Store(true)

		err := <-runDone
		if e, ok := errors.AsType[*panics.Error](err); !ok || e.Value != "corrupt icefall" || !strings.Contains(err.Error(), "map icefall") {
			t.Fatalf("Run returned %v", err)
		}
		if got := saver.attemptsSoFar(); len(got) != 1 || got[0] != "cp dune_road" {
			t.Fatalf("saves %v, want the dune road player's checkpoint", got)
		}
	})
}
