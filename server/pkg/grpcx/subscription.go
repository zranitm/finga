package grpcx

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"finga/pkg/hub"
)

// ServeSubscription serves a hub subscription on a server stream: first,
// then every message the hub sends it, in order. It returns when the
// subscription ends (replaced, overflowed or closed), as Unavailable so the
// caller subscribes again, or when the caller goes away, closing the
// subscription.
func ServeSubscription[T any](stream grpc.ServerStreamingServer[T], sub *hub.Sub[*T], first *T) error {
	defer sub.Close()
	if err := stream.Send(first); err != nil {
		return err
	}
	for {
		select {
		case m := <-sub.C():
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-sub.Done():
			return status.Error(codes.Unavailable, "subscription ended")
		case <-stream.Context().Done():
			return nil
		}
	}
}
