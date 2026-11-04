package grpctest_test

import (
	"slices"
	"testing"

	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

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

// A deprecated reason is one the service no longer sends, so its table needs no row for it.
func TestReasonsLeavesOutTheDeprecatedOnes(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: new("shop.proto"), Package: new("shop.v1"), Syntax: new("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{Name: new("Reason"), Value: []*descriptorpb.EnumValueDescriptorProto{
			{Name: new("REASON_UNSPECIFIED"), Number: new(int32(0))},
			{Name: new("REASON_SOLD_OUT"), Number: new(int32(1))},
			{Name: new("REASON_CLOSED"), Number: new(int32(2)), Options: &descriptorpb.EnumValueOptions{Deprecated: new(true)}},
		}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := grpctest.Reasons(file.Enums().Get(0))
	if want := []grpcx.Reason{{Domain: "shop.v1", Name: "REASON_SOLD_OUT"}}; !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestErrorCarriesTheReason(t *testing.T) {
	err := grpctest.Error(codes.FailedPrecondition, testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
}
