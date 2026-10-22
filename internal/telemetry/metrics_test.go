package telemetry_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"finga/internal/telemetry"
)

func TestNewMetricsRegistersEveryMetricFromPage3(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	// Touch every metric so it has a series to gather.
	m.TickDuration.WithLabelValues("town").Observe(0.004)
	m.TickOverruns.WithLabelValues("town").Inc()
	m.PlayersOnline.Set(3)
	m.WSMessages.WithLabelValues("in", "Input").Inc()
	m.WSSlowConsumerDisconnects.Inc()
	m.DBTxDuration.WithLabelValues("pickup").Observe(0.002)
	m.DBTxRetries.WithLabelValues("pickup").Inc()
	m.EconomyJobsInflight.Set(1)
	m.OutboxUnpublished.Set(12)

	tests := []struct {
		name string
	}{
		{"tick_duration_seconds"},
		{"tick_overruns_total"},
		{"players_online"},
		{"ws_messages_total"},
		{"ws_slow_consumer_disconnects_total"},
		{"db_tx_duration_seconds"},
		{"db_tx_retries_total"},
		{"economy_jobs_inflight"},
		{"outbox_unpublished"},
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range families {
		names[f.GetName()] = true
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !names[tt.name] {
				t.Fatalf("%s is not registered; have %v", tt.name, names)
			}
		})
	}
	if got := testutil.ToFloat64(m.PlayersOnline); got != 3 {
		t.Fatalf("players_online = %v", got)
	}
	if got := testutil.ToFloat64(m.WSMessages.WithLabelValues("in", "Input")); got != 1 {
		t.Fatalf("ws_messages_total{in,Input} = %v", got)
	}
}

func TestNewMetricsTwiceOnOneRegistryFails(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := telemetry.NewMetrics(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := telemetry.NewMetrics(reg); err == nil {
		t.Fatal("second registration succeeded")
	}
}

func TestObserveTick(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		took         time.Duration
		budget       time.Duration
		wantOverruns float64
	}{
		{"within budget", 10 * time.Millisecond, 50 * time.Millisecond, 0},
		{"exactly the budget", 50 * time.Millisecond, 50 * time.Millisecond, 0},
		{"over budget", 51 * time.Millisecond, 50 * time.Millisecond, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := testutil.ToFloat64(m.TickOverruns.WithLabelValues(tt.name))
			m.ObserveTick(tt.name, tt.took, tt.budget)
			got := testutil.ToFloat64(m.TickOverruns.WithLabelValues(tt.name)) - before
			if got != tt.wantOverruns {
				t.Fatalf("overruns = %v, want %v", got, tt.wantOverruns)
			}
			if n := testutil.CollectAndCount(m.TickDuration); n == 0 {
				t.Fatal("tick duration was not observed")
			}
		})
	}
}

func TestGatewayObserver(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.MessageIn("Input")
	m.MessageIn("Input")
	m.MessageOut("Delta")
	m.SlowConsumer()
	m.OnlineChanged(2)
	m.OnlineChanged(-1)
	if got := testutil.ToFloat64(m.WSMessages.WithLabelValues("in", "Input")); got != 2 {
		t.Fatalf("in Input = %v", got)
	}
	if got := testutil.ToFloat64(m.WSMessages.WithLabelValues("out", "Delta")); got != 1 {
		t.Fatalf("out Delta = %v", got)
	}
	if got := testutil.ToFloat64(m.WSSlowConsumerDisconnects); got != 1 {
		t.Fatalf("slow consumers = %v", got)
	}
	if got := testutil.ToFloat64(m.PlayersOnline); got != 1 {
		t.Fatalf("players online = %v", got)
	}
}

func TestEconomyObserver(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.JobStarted()
	m.JobStarted()
	m.JobFinished()
	if got := testutil.ToFloat64(m.EconomyJobsInflight); got != 1 {
		t.Fatalf("economy_jobs_inflight = %v, want 1", got)
	}
}
