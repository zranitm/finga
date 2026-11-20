package world_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	metrics "finga/internal/metrics/world"
	"finga/pkg/observability/metrics/promtest"
)

func TestWorldMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg, []string{"refused", "unexplained"})
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"refused", "unexplained"} {
		if v, ok := promtest.Series(t, reg, "finga_saves_dropped_total", "reason", reason); !ok || v != 0 {
			t.Fatalf("before any drop, reason %s is %v (present %v); want 0, so its first drop shows as an increase", reason, v, ok)
		}
	}
	m.ObserveTick("town", 0, 10*time.Millisecond, 50*time.Millisecond)
	m.ObserveTick("town", 0, 50*time.Millisecond, 50*time.Millisecond)
	m.ObserveTick("town", 0, 51*time.Millisecond, 50*time.Millisecond)
	m.ObserveTick("vale", 2, 60*time.Millisecond, 50*time.Millisecond)
	m.ChannelsOpen("vale", 1)
	m.ChannelsOpen("vale", 2)
	m.CallQueued()
	m.CallQueued()
	m.CallDone()
	m.ObserveBatch(3)
	m.SaveDropped("unexplained")
	if got := promtest.Sample(t, reg, "finga_saves_dropped_total", "reason", "unexplained"); got != 1 {
		t.Fatalf("saves dropped %v, want 1", got)
	}
	if got := promtest.Sample(t, reg, "finga_tick_duration_seconds", "map", "town", "channel", "0"); got != 3 {
		t.Fatalf("ticks observed %v, want 3", got)
	}
	if got := promtest.Sample(t, reg, "finga_tick_overruns_total", "map", "town", "channel", "0"); got != 1 {
		t.Fatalf("overruns %v, want only the one past the budget", got)
	}
	if got := promtest.Sample(t, reg, "finga_tick_overruns_total", "map", "vale", "channel", "2"); got != 1 {
		t.Fatalf("channel 2's overruns %v, want 1", got)
	}
	if got := promtest.Sample(t, reg, "finga_channels_open", "region", "vale"); got != 2 {
		t.Fatalf("vale's channels open %v, want 2", got)
	}
	m.ObserveTick("vale", 3, 10*time.Millisecond, 50*time.Millisecond)
	m.ChannelStopped("vale", 2)
	if _, ok := promtest.Series(t, reg, "finga_tick_overruns_total", "map", "vale", "channel", "2"); ok {
		t.Fatal("a stopped channel's overruns stay")
	}
	if _, ok := promtest.Series(t, reg, "finga_tick_duration_seconds", "map", "vale", "channel", "2"); ok {
		t.Fatal("a stopped channel's ticks stay")
	}
	if got := promtest.Sample(t, reg, "finga_tick_duration_seconds", "map", "vale", "channel", "3"); got != 1 {
		t.Fatalf("channel 3's ticks %v after 2 stopped, want 1", got)
	}
	if got := promtest.Sample(t, reg, "finga_item_calls_inflight"); got != 1 {
		t.Fatalf("finga_item_calls_inflight = %v, want 1", got)
	}
	if got := promtest.Sample(t, reg, "finga_item_calls_batch_size"); got != 1 {
		t.Fatalf("batches observed %v, want 1", got)
	}
	if _, err := metrics.New(reg, nil); err == nil {
		t.Fatal("a second registration succeeded")
	}
}
