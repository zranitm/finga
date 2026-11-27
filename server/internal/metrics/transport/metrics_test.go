package transport_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	metrics "finga/internal/metrics/transport"
	"finga/pkg/observability/metrics/promtest"
)

func TestWebSocketMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.MessageIn("Input")
	m.MessageIn("Input")
	m.MessageOut("Delta")
	m.SlowConsumer()
	for _, c := range []struct {
		name   string
		labels []string
		want   float64
	}{
		{"finga_ws_messages_total", []string{"direction", "in", "type", "Input"}, 2},
		{"finga_ws_messages_total", []string{"direction", "out", "type", "Delta"}, 1},
		{"finga_ws_slow_consumer_disconnects_total", nil, 1},
	} {
		if got := promtest.Sample(t, reg, c.name, c.labels...); got != c.want {
			t.Fatalf("%s%v = %v, want %v", c.name, c.labels, got, c.want)
		}
	}
	if have := promtest.Names(t, reg); have["finga_tick_duration_seconds"] || have["finga_db_tx_duration_seconds"] {
		t.Fatalf("the transport registers metrics it never records: %v", have)
	}
}
