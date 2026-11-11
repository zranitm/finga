package retentionprom_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/promtest"
	"finga/pkg/retention/retentionprom"
)

func TestRetentionMetricsCountDeletedRowsByStore(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := retentionprom.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.Deleted("sessions", 0)
	m.Deleted("idempotency_keys", 1000)
	m.Deleted("idempotency_keys", 7)
	if got := promtest.Sample(t, reg, "finga_retention_deleted_total", "store", "idempotency_keys"); got != 1007 {
		t.Fatalf("idempotency_keys %v, want 1007", got)
	}
	if !promtest.Names(t, reg)["finga_retention_deleted_total"] || promtest.Sample(t, reg, "finga_retention_deleted_total", "store", "sessions") != 0 {
		t.Fatal("a pass that deleted nothing left no series for its table")
	}
	if _, err := retentionprom.New(reg); err == nil {
		t.Fatal("a second registration succeeded")
	}
}
