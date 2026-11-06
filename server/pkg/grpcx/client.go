package grpcx

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Keepalive pings an idle connection so a half-open link on a long-lived stream breaks in seconds, not minutes.
const (
	keepaliveTime    = 20 * time.Second
	keepaliveTimeout = 5 * time.Second
	keepaliveMinTime = 10 * time.Second
)

// Dial makes a plain-text (internal network) client connection.
func Dial(addr string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: keepaliveTime, Timeout: keepaliveTimeout, PermitWithoutStream: true}),
	}, extra...)
	return grpc.NewClient(addr, opts...)
}
