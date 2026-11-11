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

func TestDBMetricsNameTheTransactionByItsMethod(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := dbxprom.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), grpcMethod("/finga.item.v1.ItemService/Add"))
	m.TxDone(ctx, 2*time.Millisecond, 1)
	m.TxDone(ctx, 9*time.Millisecond, 3)
	m.TxDone(context.Background(), time.Millisecond, 1)
	if got := promtest.Sample(t, reg, "finga_db_tx_duration_seconds", "operation", "Add"); got != 2 {
		t.Fatalf("Add transactions %v, want 2", got)
	}
	if got := promtest.Sample(t, reg, "finga_db_tx_retries_total", "operation", "Add"); got != 2 {
		t.Fatalf("Add retries %v, want the two re-runs", got)
	}
	if got := promtest.Sample(t, reg, "finga_db_tx_duration_seconds", "operation", "other"); got != 1 {
		t.Fatalf("transactions outside a call %v, want 1", got)
	}
}
