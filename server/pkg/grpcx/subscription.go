package grpcx

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"finga/pkg/hub"
)

// ServeSubscription streams first, then each of sub's events as msg makes it a message, until sub ends, then returns Unavailable; first may exceed sub's queue.
// An event msg has no message for, nil, sends nothing.
func ServeSubscription[E, T any](stream grpc.ServerStreamingServer[T], sub *hub.Sub[E], msg func(E) *T, first ...*T) error {
	defer sub.Close()
	for _, m := range first {
		if err := stream.Send(m); err != nil {
			return err
		}
	}
	for {
		select {
		case e := <-sub.C():
			m := msg(e)
			if m == nil {
				continue
			}
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
