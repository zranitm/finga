package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics/promtest"
	"finga/services/gateway/internal/metrics"
)

func TestGatewayMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.MessageIn("Input")
	m.MessageIn("Input")
	m.MessageOut("Delta")
	m.SlowConsumer()
	m.OnlineChanged(2)
	m.OnlineChanged(-1)
	m.RegistrationsWaiting(1)
	for _, c := range []struct {
		name   string
		labels []string
		want   float64
	}{
		{"finga_ws_messages_total", []string{"direction", "in", "type", "Input"}, 2},
		{"finga_ws_messages_total", []string{"direction", "out", "type", "Delta"}, 1},
		{"finga_ws_slow_consumer_disconnects_total", nil, 1},
		{"finga_players_online", nil, 1},
		{"finga_social_registrations_waiting", nil, 1},
	} {
		if got := promtest.Sample(t, reg, c.name, c.labels...); got != c.want {
			t.Fatalf("%s%v = %v, want %v", c.name, c.labels, got, c.want)
		}
	}
	if have := promtest.Names(t, reg); have["finga_tick_duration_seconds"] || have["finga_db_tx_duration_seconds"] {
		t.Fatalf("the gateway registers metrics it never records: %v", have)
	}
}
