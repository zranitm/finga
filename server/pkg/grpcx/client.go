package grpcx

import (
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Keepalive pings an idle connection so a half-open link on a long-lived stream breaks in seconds, not minutes.
const (
	keepaliveTime    = 20 * time.Second
	keepaliveTimeout = 5 * time.Second
	keepaliveMinTime = 10 * time.Second
)

// A dependency that is down is tried again at least every few seconds; gRPC's default backoff grows to 2 minutes, and calls would fail for that long after it was back.
var reconnect = grpc.ConnectParams{
	Backoff:           backoff.Config{BaseDelay: 200 * time.Millisecond, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 3 * time.Second},
	MinConnectTimeout: 5 * time.Second,
}

// Dial makes a plain-text (internal network) client connection.
// A bare host:port is looked up at each connect: gRPC's DNS resolver would keep a lookup that failed while a restarting container had no record, and calls would fail for up to 30 s after it was back.
func Dial(addr string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: keepaliveTime, Timeout: keepaliveTimeout, PermitWithoutStream: true}),
		grpc.WithConnectParams(reconnect),
	}, extra...)
	if !strings.Contains(addr, "://") {
		addr = "passthrough:///" + addr
	}
	return grpc.NewClient(addr, opts...)
}
