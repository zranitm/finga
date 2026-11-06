package grpcx

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"finga/pkg/hub"
)

// ServeSubscription streams first, then sub until it ends, then returns Unavailable; first may exceed sub's queue.
func ServeSubscription[T any](stream grpc.ServerStreamingServer[T], sub *hub.Sub[*T], first ...*T) error {
	defer sub.Close()
	for _, m := range first {
		if err := stream.Send(m); err != nil {
			return err
		}
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
