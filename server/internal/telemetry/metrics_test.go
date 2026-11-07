package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"finga/internal/telemetry"
)

// sample is the value of the series name{labels} on reg: a counter's or gauge's value, a histogram's count; 0 when there is none.
func sample(t *testing.T, reg *prometheus.Registry, name string, labels ...string) float64 {
	t.Helper()
	v, _ := series(t, reg, name, labels...)
	return v
}

// series is the value of the series name{labels} on reg, as sample, and whether reg has it.
func series(t *testing.T, reg *prometheus.Registry, name string, labels ...string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if matches(m, labels) {
				switch {
				case m.GetCounter() != nil:
					return m.GetCounter().GetValue(), true
				case m.GetGauge() != nil:
					return m.GetGauge().GetValue(), true
				case m.GetHistogram() != nil:
					return float64(m.GetHistogram().GetSampleCount()), true
				}
			}
		}
	}
	return 0, false
}

func matches(m *dto.Metric, labels []string) bool {
	have := map[string]string{}
	for _, l := range m.GetLabel() {
		have[l.GetName()] = l.GetValue()
	}
	for i := 0; i+1 < len(labels); i += 2 {
		if have[labels[i]] != labels[i+1] {
			return false
		}
	}
	return true
}

func names(t *testing.T, reg *prometheus.Registry) map[string]bool {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range families {
		out[f.GetName()] = true
	}
	return out
}

func TestWorldMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewWorldMetrics(reg, []string{"refused", "unexplained"})
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"refused", "unexplained"} {
		if v, ok := series(t, reg, "finga_saves_dropped_total", "reason", reason); !ok || v != 0 {
			t.Fatalf("before any drop, reason %s is %v (present %v); want 0, so its first drop shows as an increase", reason, v, ok)
		}
	}
	m.ObserveTick("town", 10*time.Millisecond, 50*time.Millisecond)
	m.ObserveTick("town", 50*time.Millisecond, 50*time.Millisecond)
	m.ObserveTick("town", 51*time.Millisecond, 50*time.Millisecond)
	m.JobStarted()
	m.JobStarted()
	m.JobFinished()
	m.SaveDropped("unexplained")
	if got := sample(t, reg, "finga_saves_dropped_total", "reason", "unexplained"); got != 1 {
		t.Fatalf("saves dropped %v, want 1", got)
	}
	if got := sample(t, reg, "finga_tick_duration_seconds", "map", "town"); got != 3 {
		t.Fatalf("ticks observed %v, want 3", got)
	}
	if got := sample(t, reg, "finga_tick_overruns_total", "map", "town"); got != 1 {
		t.Fatalf("overruns %v, want only the one past the budget", got)
	}
	if got := sample(t, reg, "finga_economy_jobs_inflight"); got != 1 {
		t.Fatalf("finga_economy_jobs_inflight = %v, want 1", got)
	}
	if _, err := telemetry.NewWorldMetrics(reg, nil); err == nil {
		t.Fatal("a second registration succeeded")
	}
}

func TestGatewayMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewGatewayMetrics(reg)
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
		if got := sample(t, reg, c.name, c.labels...); got != c.want {
			t.Fatalf("%s%v = %v, want %v", c.name, c.labels, got, c.want)
		}
	}
	if have := names(t, reg); have["finga_tick_duration_seconds"] || have["finga_db_tx_duration_seconds"] {
		t.Fatalf("the gateway registers metrics it never records: %v", have)
	}
}

// grpcMethod is a context as a gRPC server hands it to a handler of method.
type grpcMethod string

func (m grpcMethod) Method() string             { return string(m) }
func (grpcMethod) SetHeader(metadata.MD) error  { return nil }
func (grpcMethod) SendHeader(metadata.MD) error { return nil }
func (grpcMethod) SetTrailer(metadata.MD) error { return nil }

func TestDBMetricsNameTheTransactionByItsMethod(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewDBMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), grpcMethod("/finga.item.v1.ItemService/Add"))
	m.TxDone(ctx, 2*time.Millisecond, 1)
	m.TxDone(ctx, 9*time.Millisecond, 3)
	m.TxDone(context.Background(), time.Millisecond, 1)
	if got := sample(t, reg, "finga_db_tx_duration_seconds", "operation", "Add"); got != 2 {
		t.Fatalf("Add transactions %v, want 2", got)
	}
	if got := sample(t, reg, "finga_db_tx_retries_total", "operation", "Add"); got != 2 {
		t.Fatalf("Add retries %v, want the two re-runs", got)
	}
	if got := sample(t, reg, "finga_db_tx_duration_seconds", "operation", "other"); got != 1 {
		t.Fatalf("transactions outside a call %v, want 1", got)
	}
}

func TestRetentionMetricsCountDeletedRowsByStore(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := telemetry.NewRetentionMetrics(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.Deleted("sessions", 0)
	m.Deleted("idempotency_keys", 1000)
	m.Deleted("idempotency_keys", 7)
	if got := sample(t, reg, "finga_retention_deleted_total", "store", "idempotency_keys"); got != 1007 {
		t.Fatalf("idempotency_keys %v, want 1007", got)
	}
	if !names(t, reg)["finga_retention_deleted_total"] || sample(t, reg, "finga_retention_deleted_total", "store", "sessions") != 0 {
		t.Fatal("a pass that deleted nothing left no series for its table")
	}
	if _, err := telemetry.NewRetentionMetrics(reg); err == nil {
		t.Fatal("a second registration succeeded")
	}
}

func TestTheOutboxGaugeFollowsItsCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m, err := telemetry.NewOutboxMetrics(reg)
		if err != nil {
			t.Fatal(err)
		}
		counts := []int64{12, -1, 30}
		count := func(context.Context) (int64, error) {
			n := counts[0]
			if len(counts) > 1 {
				counts = counts[1:]
			}
			if n < 0 {
				return 0, errors.New("database down")
			}
			return n, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error)
		go func() { done <- m.Track(ctx, time.Minute, count, slog.New(slog.DiscardHandler)) }()
		synctest.Wait()
		if got := sample(t, reg, "finga_outbox_unpublished"); got != 12 {
			t.Fatalf("finga_outbox_unpublished = %v, want 12", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := sample(t, reg, "finga_outbox_unpublished"); got != 12 {
			t.Fatalf("after a failed count %v, want the last good one", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := sample(t, reg, "finga_outbox_unpublished"); got != 30 {
			t.Fatalf("finga_outbox_unpublished = %v, want 30", got)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
