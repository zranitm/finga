package grpcx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/hub"
	"finga/pkg/logger"
	"finga/pkg/panics"
	"finga/pkg/reqid"
)

var (
	errFull     = errors.New("shop: full")
	errNotFound = errors.New("shop: not found")
	errBusy     = errors.New("shop: the till timed out")
	errSecret   = errors.New("database password is hunter2")
)

var table = grpcx.ErrorTable{
	{Err: errFull, Code: codes.FailedPrecondition, Reason: testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND},
	{Err: errNotFound, Code: codes.NotFound, Reason: testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK},
	{Err: errBusy, Code: codes.Unavailable},
}

// shop is the test service: EmptyCall returns err, UnaryCall panics, StreamingOutputCall streams.
type shop struct {
	testpb.UnimplementedTestServiceServer
	err    func(ctx context.Context) error
	sawID  chan string
	stream func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error
}

func (s *shop) EmptyCall(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
	if s.sawID != nil {
		s.sawID <- reqid.From(ctx)
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
		{"a sentinel with no reason", fmt.Errorf("buying: %w", errBusy), codes.Unavailable, grpcx.Reason{}, false},
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

// A row without a reason is a failure a retry may fix: explained, so it is no bug to log, and sent as its code alone.
func TestARowWithoutAReasonSendsItsCodeAlone(t *testing.T) {
	st, explained := table.Status(fmt.Errorf("buying: %w", errBusy))
	if st.Code() != codes.Unavailable || len(st.Details()) != 0 || !explained || strings.Contains(st.Message(), "till") {
		t.Fatalf("status %v with %d details, explained %v", st, len(st.Details()), explained)
	}
}

func TestKeyNamesTheEnumValueAndItsPackage(t *testing.T) {
	k := grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND)
	if k.Domain != "grpc.testing" || k.Name != "GRPCLB_ROUTE_TYPE_BACKEND" || k.String() != "grpc.testing/GRPCLB_ROUTE_TYPE_BACKEND" {
		t.Fatalf("key %+v (%s)", k, k)
	}
}

func TestAReasonFromDownstreamPassesThrough(t *testing.T) {
	// A downstream error passed on as-is keeps its reason and code.
	downstream := serve(t, &shop{err: func(context.Context) error { return errNotFound }})
	upstream := serve(t, &shop{err: func(ctx context.Context) error {
		_, err := downstream.EmptyCall(ctx, &testpb.Empty{})
		return fmt.Errorf("asking downstream: %w", err)
	}})
	_, err := upstream.EmptyCall(context.Background(), &testpb.Empty{})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK) || status.Code(err) != codes.NotFound {
		t.Fatalf("got %v, reason %+v", err, r)
	}
	// The upstream's own wrapping is its text, which never leaves it.
	if strings.Contains(err.Error(), "asking downstream") {
		t.Fatalf("the upstream's text crossed the call: %v", err)
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
	// Unimplemented (an old node mid-deploy) must not hide behind Internal.
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
	var logs bytes.Buffer
	log, err := logger.New(&logs, logger.Config{Level: "info", Format: logger.FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table, Panics: panics.NewRecorder(log, nil)})
	testpb.RegisterTestServiceServer(srv, &shop{})
	c := testpb.NewTestServiceClient(grpctest.Conn(t, srv))
	ctx := reqid.With(context.Background(), "r-7")
	if _, err := c.UnaryCall(ctx, &testpb.SimpleRequest{}); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
	if _, err := c.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
		t.Fatalf("after the panic: %v", err)
	}
	for _, want := range []string{`"msg":"panic recovered"`, `"flow":"grpc"`, `"panic":"boom"`, `"method":"/grpc.testing.TestService/UnaryCall"`, `"request_id":"r-7"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q lacks %s", logs.String(), want)
		}
	}
}

func TestTheRequestIDCrossesTheCall(t *testing.T) {
	s := &shop{sawID: make(chan string, 2)}
	c := serve(t, s)
	if _, err := c.EmptyCall(reqid.With(context.Background(), "r-42"), &testpb.Empty{}); err != nil {
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

// Draining ends a subscription that would never end on its own, so GracefulStop returns at once.
func TestDrainingEndsOpenStreamsWithUnavailable(t *testing.T) {
	draining := make(chan struct{})
	var logs bytes.Buffer
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table, Logger: slog.New(slog.NewTextHandler(&logs, nil)), Draining: draining})
	subs := hub.New[*testpb.StreamingOutputCallResponse](4)
	testpb.RegisterTestServiceServer(srv, &shop{stream: func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
		return grpcx.ServeSubscription(stream, subs.Subscribe("gateway"), &testpb.StreamingOutputCallResponse{})
	}})
	c := testpb.NewTestServiceClient(grpctest.Conn(t, srv))
	stream, err := c.StreamingOutputCall(context.Background(), &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	close(draining)
	if _, err := stream.Recv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("after the drain the stream got %v", err)
	}
	stopped := make(chan struct{})
	go func() { srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("GracefulStop still waits for the stream")
	}
	if logs.Len() > 0 {
		t.Fatalf("a drain is no failure, yet it logged %s", logs.String())
	}
}

// The metrics see a call as it leaves: a sentinel counts under the code the table gives it.
func TestMetricsCountTheCodeTheCallerSees(t *testing.T) {
	metrics := grpcx.NewServerMetrics()
	reg := prometheus.NewRegistry()
	reg.MustRegister(metrics)
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table, Metrics: metrics})
	testpb.RegisterTestServiceServer(srv, &shop{err: func(context.Context) error { return errFull }})
	c := testpb.NewTestServiceClient(grpctest.Conn(t, srv))
	if _, err := c.EmptyCall(context.Background(), &testpb.Empty{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
	if code := handledAs(t, reg, "grpc_server_handled_total", "EmptyCall"); code != "FailedPrecondition" {
		t.Fatalf("counted as %q", code)
	}
}

// The caller counts its calls too, under the code it got: a service down shows on the caller's side.
func TestClientMetricsCountTheCodeTheCallerGot(t *testing.T) {
	metrics := grpcx.NewClientMetrics()
	reg := prometheus.NewRegistry()
	reg.MustRegister(metrics)
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: table})
	testpb.RegisterTestServiceServer(srv, &shop{err: func(context.Context) error { return errFull }})
	c := testpb.NewTestServiceClient(grpctest.Conn(t, srv, grpcx.ClientMetrics(metrics)...))
	if _, err := c.EmptyCall(context.Background(), &testpb.Empty{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("got %v", err)
	}
	if code := handledAs(t, reg, "grpc_client_handled_total", "EmptyCall"); code != "FailedPrecondition" {
		t.Fatalf("counted as %q", code)
	}
}

// handledAs is the code the one call of method was counted under in family; empty when it was not.
func handledAs(t *testing.T, reg *prometheus.Registry, family, method string) string {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != family {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["grpc_method"] == method && m.GetCounter().GetValue() == 1 {
				return labels["grpc_code"]
			}
		}
	}
	return ""
}

func TestFollow(t *testing.T) {
	// Each subscription sends three messages and ends; Follow must resubscribe.
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

func TestErrorForBuildsAFailureWithAReason(t *testing.T) {
	err := grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND))
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

func TestServeSubscription(t *testing.T) {
	events := hub.New[*testpb.StreamingOutputCallResponse](4)
	c := serve(t, &shop{stream: func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
		first := &testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: []byte("subscribed")}}
		return grpcx.ServeSubscription(stream, events.Subscribe("gw-1"), first)
	}})
	body := func(m *testpb.StreamingOutputCallResponse) string { return string(m.GetPayload().GetBody()) }

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || body(m) != "subscribed" {
		t.Fatalf("first %v %v", m, err)
	}
	t.Run("what the hub sends arrives in order", func(t *testing.T) {
		for _, b := range []string{"a", "b"} {
			sub, ok := events.Get("gw-1")
			if !ok || !sub.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: []byte(b)}}) {
				t.Fatal("no subscription")
			}
		}
		for _, want := range []string{"a", "b"} {
			if m, err := stream.Recv(); err != nil || body(m) != want {
				t.Fatalf("got %v %v, want %s", m, err, want)
			}
		}
	})
	t.Run("a replaced subscription ends the stream as Unavailable", func(t *testing.T) {
		events.Subscribe("gw-1")
		if _, err := stream.Recv(); status.Code(err) != codes.Unavailable {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("the caller going away ends the subscription", func(t *testing.T) {
		stream, err := c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatal(err)
		}
		sub, _ := events.Get("gw-1")
		cancel()
		select {
		case <-sub.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the subscription outlived its caller")
		}
	})
}

func TestAPanicInHandleBreaksTheStreamAndFollowResubscribes(t *testing.T) {
	// Each subscription sends two messages and stays open, so only the panic can end the first.
	var mu sync.Mutex
	subscriptions := 0
	ended := make(chan struct{}, 10)
	c := serve(t, &shop{stream: func(stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse]) error {
		mu.Lock()
		subscriptions++
		n := subscriptions
		mu.Unlock()
		for i := range 2 {
			if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: &testpb.Payload{Body: fmt.Appendf(nil, "%d.%d", n, i)}}); err != nil {
				return err
			}
		}
		<-stream.Context().Done()
		ended <- struct{}{}
		return nil
	}})
	var logs bytes.Buffer
	var logsMu sync.Mutex
	rec := panics.NewRecorder(slog.New(slog.NewJSONHandler(writerFunc(func(p []byte) (int, error) {
		logsMu.Lock()
		defer logsMu.Unlock()
		return logs.Write(p)
	}), nil)), nil)
	ctx := t.Context()
	got := make(chan string, 100)
	breaks := make(chan error, 100)
	go grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[testpb.StreamingOutputCallResponse], error) {
		return c.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
	}, func(m *testpb.StreamingOutputCallResponse) {
		body := string(m.GetPayload().GetBody())
		if body == "1.1" {
			panic("bad delivery")
		}
		got <- body
	}, grpcx.FollowOptions{OnBreak: func(err error) { breaks <- err }, Panics: rec, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond})

	var seen []string
	for len(seen) < 3 {
		select {
		case m := <-got:
			seen = append(seen, m)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %v", seen)
		}
	}
	if strings.Join(seen, " ") != "1.0 2.0 2.1" {
		t.Fatalf("messages %v", seen)
	}
	if err := <-breaks; err == nil || !strings.Contains(err.Error(), "panic: bad delivery") {
		t.Fatalf("break %v", err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the broken subscription's stream was left open")
	}
	logsMu.Lock()
	defer logsMu.Unlock()
	if !strings.Contains(logs.String(), `"flow":"stream"`) || !strings.Contains(logs.String(), `"panic":"bad delivery"`) {
		t.Fatalf("log %q", logs.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unavailable", status.Error(codes.Unavailable, "down"), true},
		{"busy, with a reason", grpcx.ErrorFor(codes.Unavailable, grpcx.Reason{Domain: "world", Name: "BUSY"}), true},
		{"out of time", status.Error(codes.DeadlineExceeded, "slow"), true},
		{"aborted", status.Error(codes.Aborted, "lock timeout"), true},
		{"exhausted", status.Error(codes.ResourceExhausted, "too many"), true},
		{"cancelled", status.Error(codes.Canceled, "shutting down"), true},
		{"a local deadline", fmt.Errorf("save: %w", context.DeadlineExceeded), true},
		{"a rule", grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Reason{Domain: "item", Name: "INVENTORY_FULL"}), false},
		{"internal", status.Error(codes.Internal, "bug"), false},
		{"not a status", errors.New("bad reply"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := grpcx.IsTransient(tt.err); got != tt.want {
				t.Fatalf("IsTransient = %v", got)
			}
		})
	}
}
