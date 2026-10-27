package grpcx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

// The sentinels a pretend service returns, and its table.
var (
	errFull     = errors.New("shop: full")
	errNotFound = errors.New("shop: not found")
	errSecret   = errors.New("database password is hunter2")
)

var table = grpcx.ErrorTable{
	{Err: errFull, Code: codes.FailedPrecondition, Reason: testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND},
	{Err: errNotFound, Code: codes.NotFound, Reason: testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK},
}

// shop is the test service: EmptyCall returns whatever err holds,
// UnaryCall panics, StreamingOutputCall sends n messages then ends.
type shop struct {
	testpb.UnimplementedTestServiceServer
	err    func(ctx context.Context) error
	sawID  chan string
	stream func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error
}

func (s *shop) EmptyCall(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
	if s.sawID != nil {
		s.sawID <- grpcx.RequestID(ctx)
	}
	if s.err != nil {
		if err := s.err(ctx); err != nil {
			return nil, err
		}
	}
	return &testpb.Empty{}, nil
}

func (s *shop) UnaryCall(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	panic("boom")
}

func (s *shop) StreamingOutputCall(_ *testpb.StreamingOutputCallRequest, stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
	return s.stream(stream)
}

func serve(t *testing.T, s *shop) testpb.TestServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table, Logger: slog.New(slog.DiscardHandler)})
	testpb.RegisterTestServiceServer(srv, s)
	return testpb.NewTestServiceClient(grpctest.Conn(t, srv))
}

func TestErrorsKeepTheirReasonAcrossTheCall(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		want     grpcx.Reason
		rule     bool
	}{
		{"a sentinel", errFull, codes.FailedPrecondition, grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND), true},
		{"another sentinel", errNotFound, codes.NotFound, grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK), true},
		{"a wrapped sentinel", fmt.Errorf("buying: %w", fmt.Errorf("stock: %w", errFull)), codes.FailedPrecondition, grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND), true},
		{"an unknown error", errSecret, codes.Internal, grpcx.Reason{}, false},
		{"a cancelled context", context.Canceled, codes.Canceled, grpcx.Reason{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serve(t, &shop{err: func(context.Context) error { return tt.err }})
			_, err := c.EmptyCall(context.Background(), &testpb.Empty{})
			if got := status.Code(err); got != tt.wantCode {
				t.Fatalf("code %v, want %v (%v)", got, tt.wantCode, err)
			}
			r, ok := grpcx.ReasonOf(err)
			if ok != tt.rule || r != tt.want {
				t.Fatalf("reason %+v %v, want %+v %v", r, ok, tt.want, tt.rule)
			}
			if grpcx.IsRule(err) != tt.rule {
				t.Fatalf("IsRule = %v", !tt.rule)
			}
			if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "buying") {
				t.Fatalf("the error's text crossed the call: %v", err)
			}
		})
	}
}

func TestKeyNamesTheEnumValueAndItsPackage(t *testing.T) {
	k := grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND)
	if k.Domain != "grpc.testing" || k.Name != "GRPCLB_ROUTE_TYPE_BACKEND" || k.String() != "grpc.testing/GRPCLB_ROUTE_TYPE_BACKEND" {
		t.Fatalf("key %+v (%s)", k, k)
	}
}

func TestAReasonFromDownstreamPassesThrough(t *testing.T) {
	// A service that calls another and returns its error as it came keeps
	// the downstream reason and code.
	downstream := serve(t, &shop{err: func(context.Context) error { return errNotFound }})
	upstream := serve(t, &shop{err: func(ctx context.Context) error {
		_, err := downstream.EmptyCall(ctx, &testpb.Empty{})
		return fmt.Errorf("asking downstream: %w", err)
	}})
	_, err := upstream.EmptyCall(context.Background(), &testpb.Empty{})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK) || status.Code(err) != codes.NotFound {
		t.Fatalf("got %v, reason %+v", err, r)
	}
}

func TestAnUnavailableDownstreamStaysUnavailable(t *testing.T) {
	upstream := serve(t, &shop{err: func(context.Context) error {
		return fmt.Errorf("asking downstream: %w", status.Error(codes.Unavailable, "connection refused to 10.0.0.7"))
	}})
	_, err := upstream.EmptyCall(context.Background(), &testpb.Empty{})
	if status.Code(err) != codes.Unavailable || grpcx.IsRule(err) || strings.Contains(err.Error(), "10.0.0.7") {
		t.Fatalf("got %v", err)
	}
}

func TestAStatusWithoutAReasonKeepsItsCodeAndLosesItsText(t *testing.T) {
	// An unimplemented call (an old node during a rolling deploy) must say
	// so, not hide behind Internal.
	c := serve(t, &shop{})
	if _, err := c.UnimplementedCall(context.Background(), &testpb.Empty{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("got %v", err)
	}
	upstream := serve(t, &shop{err: func(context.Context) error {
		return fmt.Errorf("asking downstream: %w", status.Error(codes.NotFound, "row 42 in table secrets"))
	}})
	_, err := upstream.EmptyCall(context.Background(), &testpb.Empty{})
	if status.Code(err) != codes.NotFound || grpcx.IsRule(err) || strings.Contains(err.Error(), "secrets") {
		t.Fatalf("got %v", err)
	}
}

func TestAPanicIsInternalAndTheServerKeepsServing(t *testing.T) {
	c := serve(t, &shop{})
	if _, err := c.UnaryCall(context.Background(), &testpb.SimpleRequest{}); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	if _, err := c.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("after the panic: %v", err)
	}
}

func TestTheRequestIDCrossesTheCall(t *testing.T) {
	s := &shop{sawID: make(chan string, 2)}
	c := serve(t, s)
	if _, err := c.EmptyCall(grpcx.WithRequestID(context.Background(), "r-42"), &testpb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if got := <-s.sawID; got != "r-42" {
		t.Fatalf("handler saw %q", got)
	}
	if _, err := c.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if got := <-s.sawID; got != "" {
		t.Fatalf("a call without one carried %q", got)
	}
}

func TestStreamErrorsUseTheTableToo(t *testing.T) {
	c := serve(t, &shop{stream: func(grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error { return errFull }})
	stream, err := c.StreamingOutputCall(context.Background(), &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND) {
		t.Fatalf("got %v", err)
	}
}

func TestFollow(t *testing.T) {
	// Each subscription sends three messages numbered by subscription, then
	// ends the stream; Follow delivers them all, reports each break, and
	// subscribes again until its context ends.
	var mu sync.Mutex
	subscriptions := 0
	c := serve(t, &shop{stream: func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
		mu.Lock()
		subscriptions++
		n := subscriptions
		mu.Unlock()
		for i := range 3 {
			body := []byte(fmt.Sprintf("%d.%d", n, i))
			if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: body}}); err != nil {
				return err
			}
		}
		return io.EOF
	}})
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan string, 100)
	breaks := make(chan error, 100)
	done := make(chan struct{})
	go func() {
		defer close(done)
		grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[testpb.StreamingOutputCallResponse], error) {
			return c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
		}, func(m *testpb.StreamingOutputCallResponse) {
			got <- string(m.GetPayload().GetBody())
		}, grpcx.FollowOptions{OnBreak: func(err error) { breaks <- err }, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond})
	}()
	var seen []string
	for len(seen) < 6 {
		select {
		case m := <-got:
			seen = append(seen, m)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %v", seen)
		}
	}
	if strings.Join(seen, " ") != "1.0 1.1 1.2 2.0 2.1 2.2" {
		t.Fatalf("messages %v", seen)
	}
	if err := <-breaks; err == nil {
		t.Fatal("a break reported no error")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not stop with its context")
	}
}

func TestErrorBuildsAFailureWithAReason(t *testing.T) {
	err := grpcx.Error(codes.FailedPrecondition, testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
	// It crosses a call like a table row does.
	c := serve(t, &shop{err: func(context.Context) error { return err }})
	_, got := c.EmptyCall(context.Background(), &testpb.Empty{})
	if r, ok := grpcx.ReasonOf(got); !ok || r.Name != "GRPCLB_ROUTE_TYPE_BACKEND" {
		t.Fatalf("after a call: %v", got)
	}
}

func TestReasonsListsEveryValueButTheZeroOne(t *testing.T) {
	got := grpcx.Reasons(testpb.GrpclbRouteType(0).Descriptor())
	want := []grpcx.Reason{
		{Domain: "grpc.testing", Name: "GRPCLB_ROUTE_TYPE_FALLBACK"},
		{Domain: "grpc.testing", Name: "GRPCLB_ROUTE_TYPE_BACKEND"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v", got)
	}
}
