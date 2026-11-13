package host

import (
	"context"

	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// health is gRPC's health service, whose watchers hear NOT_SERVING before the drain ends their stream.
type health struct {
	*grpchealth.Server
	drainCtx context.Context // done once the drain starts
}

// Watch streams a service's status; a watch the drain ended without NOT_SERVING sends it last.
func (h health) Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	sent := &lastSent{Health_WatchServer: stream}
	err := h.Server.Watch(in, sent)
	select {
	case <-h.drainCtx.Done():
		if sent.status != healthpb.HealthCheckResponse_NOT_SERVING {
			_ = stream.Send(&healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}) // a watcher already gone hears nothing
		}
	default:
	}
	return err
}

// lastSent is a watch's stream that remembers the last status it sent.
type lastSent struct {
	healthpb.Health_WatchServer
	status healthpb.HealthCheckResponse_ServingStatus
}

// Send sends m and remembers its status.
func (s *lastSent) Send(m *healthpb.HealthCheckResponse) error {
	s.status = m.GetStatus()
	return s.Health_WatchServer.Send(m)
}
