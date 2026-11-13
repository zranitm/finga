package host

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// slowWatcher is a watch's stream whose first status is still being sent until release closes.
type slowWatcher struct {
	grpc.ServerStream
	ctx      context.Context
	sending  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	statuses []healthpb.HealthCheckResponse_ServingStatus
}

func (s *slowWatcher) Context() context.Context { return s.ctx }

func (s *slowWatcher) Send(m *healthpb.HealthCheckResponse) error {
	s.mu.Lock()
	s.statuses = append(s.statuses, m.GetStatus())
	first := len(s.statuses) == 1
	s.mu.Unlock()
	if first {
		close(s.sending)
		<-s.release
	}
	return nil
}

func (s *slowWatcher) last() healthpb.HealthCheckResponse_ServingStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statuses[len(s.statuses)-1]
}

// A watcher still being sent its first status when the drain begins hears NOT_SERVING before its stream ends.
func TestAWatcherHearsNotServingBeforeTheDrainEndsIt(t *testing.T) {
	for range 50 {
		healthy := grpchealth.NewServer()
		drainCtx, drain := context.WithCancel(context.Background())
		h := health{Server: healthy, drainCtx: drainCtx}
		ctx, endStream := context.WithCancel(context.Background())
		s := &slowWatcher{ctx: ctx, sending: make(chan struct{}), release: make(chan struct{})}
		done := make(chan struct{})
		go func() { defer close(done); _ = h.Watch(&healthpb.HealthCheckRequest{}, s) }()
		<-s.sending
		healthy.Shutdown()
		drain()
		endStream() // as the drain ends every stream
		close(s.release)
		<-done
		if last := s.last(); last != healthpb.HealthCheckResponse_NOT_SERVING {
			t.Fatalf("the watcher last heard %s", last)
		}
	}
}
