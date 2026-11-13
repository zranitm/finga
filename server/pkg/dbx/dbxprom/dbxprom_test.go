package dbxprom_test

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"finga/pkg/dbx/dbxprom"
	"finga/pkg/observability/metrics/promtest"
)

// grpcMethod is a context as a gRPC server hands it to a handler of method.
type grpcMethod string

func (m grpcMethod) Method() string             { return string(m) }
func (grpcMethod) SetHeader(metadata.MD) error  { return nil }
func (grpcMethod) SendHeader(metadata.MD) error { return nil }
func (grpcMethod) SetTrailer(metadata.MD) error { return nil }

func TestDBMetricsNameTheTransactionByItsModuleAndMethod(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := dbxprom.New(reg, "item")
	if err != nil {
		t.Fatal(err)
	}
	other, err := dbxprom.New(reg, "account")
	if err != nil {
		t.Fatal(err)
	}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), grpcMethod("/finga.item.v1.ItemService/Add"))
	m.TxDone(ctx, 2*time.Millisecond, 1)
	m.TxDone(ctx, 9*time.Millisecond, 3)
	m.TxDone(context.Background(), time.Millisecond, 1)
	other.TxDone(context.Background(), time.Millisecond, 1)
	if got := promtest.Sample(t, reg, "finga_db_tx_duration_seconds", "module", "item", "operation", "Add"); got != 2 {
		t.Fatalf("Add transactions %v, want 2", got)
	}
	if got := promtest.Sample(t, reg, "finga_db_tx_retries_total", "module", "item", "operation", "Add"); got != 2 {
		t.Fatalf("Add retries %v, want the two re-runs", got)
	}
	for _, module := range []string{"item", "account"} {
		if got := promtest.Sample(t, reg, "finga_db_tx_duration_seconds", "module", module, "operation", "other"); got != 1 {
			t.Fatalf("%s's transactions outside a call %v, want 1", module, got)
		}
	}
}
