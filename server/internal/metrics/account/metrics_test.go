package account_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	metrics "finga/internal/metrics/account"
	"finga/pkg/observability/metrics/promtest"
)

func TestConnectionMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.OnlineChanged(2)
	m.OnlineChanged(-1)
	m.RegistrationsWaiting(1)
	for _, c := range []struct {
		name string
		want float64
	}{
		{"finga_players_online", 1},
		{"finga_social_registrations_waiting", 1},
	} {
		if got := promtest.Sample(t, reg, c.name); got != c.want {
			t.Fatalf("%s = %v, want %v", c.name, got, c.want)
		}
	}
	if have := promtest.Names(t, reg); have["finga_tick_duration_seconds"] || have["finga_db_tx_duration_seconds"] {
		t.Fatalf("the connections register metrics they never record: %v", have)
	}
}
