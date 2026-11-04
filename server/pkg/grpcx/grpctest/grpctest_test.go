package grpctest_test

import (
	"slices"
	"testing"

	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

func TestReasonsListsEveryValueButTheZeroOne(t *testing.T) {
	got := grpctest.Reasons(testpb.GrpclbRouteType(0).Descriptor())
	want := []grpcx.Reason{
		{Domain: "grpc.testing", Name: "GRPCLB_ROUTE_TYPE_FALLBACK"},
		{Domain: "grpc.testing", Name: "GRPCLB_ROUTE_TYPE_BACKEND"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestErrorCarriesTheReason(t *testing.T) {
	err := grpctest.Error(codes.FailedPrecondition, testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
}
