package grpcx_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/grpc"

	"finga/pkg/grpcx"
)

// brief is a subscription that delivers one message, stays open for lasts, then breaks.
type brief struct {
	grpc.ClientStream
	lasts time.Duration
	sent  bool
}

func (b *brief) Recv() (*string, error) {
	if !b.sent {
		b.sent = true
		s := "subscribed"
		return &s, nil
	}
	time.Sleep(b.lasts)
	return nil, errors.New("stream broke")
}

// subscriptions counts what Follow opens in ten seconds, each subscription lasting the next of lasts.
func subscriptions(t *testing.T, lasts ...time.Duration) (opened int32, at []time.Duration) {
	t.Helper()
	var n atomic.Int32
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	grpcx.Follow(ctx, func(context.Context) (grpc.ServerStreamingClient[string], error) {
		i := int(n.Add(1)) - 1
		at = append(at, time.Since(start))
		return &brief{lasts: lasts[min(i, len(lasts)-1)]}, nil
	}, func(*string) {}, grpcx.FollowOptions{MinBackoff: 100 * time.Millisecond, MaxBackoff: 5 * time.Second})
	return n.Load(), at
}

func TestFollowBacksOffAStreamThatBreaksRightAfterSubscribing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Each wait doubles from 100 ms toward 5 s; resetting on Subscribed would retry about every 100 ms.
		if n, _ := subscriptions(t, 0); n > 12 {
			t.Fatalf("%d subscriptions in ten seconds: the backoff never grew", n)
		}
	})
}

func TestFollowStartsOverAfterASubscriptionThatHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Two quick breaks grow the wait; the third subscription holds six seconds, so the next wait is short again.
		_, at := subscriptions(t, 0, 0, 6*time.Second, 0)
		if len(at) < 4 {
			t.Fatalf("subscriptions at %v", at)
		}
		if wait := at[3] - at[2] - 6*time.Second; wait > 150*time.Millisecond {
			t.Fatalf("waited %s after a subscription that held, want the first backoff again", wait)
		}
	})
}
