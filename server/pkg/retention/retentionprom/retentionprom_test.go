package retentionprom_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/observability/metrics/promtest"
	"finga/pkg/retention/retentionprom"
)

func TestRetentionMetricsCountDeletedRowsByModuleAndStore(t *testing.T) {
	reg := prometheus.NewRegistry()
	item, err := retentionprom.New(reg, "item")
	if err != nil {
		t.Fatal(err)
	}
	account, err := retentionprom.New(reg, "account")
	if err != nil {
		t.Fatal(err)
	}
	account.Deleted("sessions", 0)
	item.Deleted("idempotency_keys", 1000)
	item.Deleted("idempotency_keys", 7)
	if got := promtest.Sample(t, reg, "finga_retention_deleted_total", "module", "item", "store", "idempotency_keys"); got != 1007 {
		t.Fatalf("idempotency_keys %v, want 1007", got)
	}
	if _, ok := promtest.Series(t, reg, "finga_retention_deleted_total", "module", "account", "store", "sessions"); !ok {
		t.Fatal("a pass that deleted nothing left no series for its table")
	}
}
