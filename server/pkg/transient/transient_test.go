package transient_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/transient"
)

// A call past its time fails as unavailable; one that answers in time keeps its answer, and its own error; a caller that gives up first keeps its own reason.
func TestWithin(t *testing.T) {
	waits := func(ctx context.Context) (int, error) { <-ctx.Done(); return 0, ctx.Err() }
	synctest.Test(t, func(t *testing.T) {
		if _, err := transient.Within(t.Context(), time.Second, "the module", waits); !errors.Is(err, transient.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("past its time: %v, want unavailable", err)
		}
		if n, err := transient.Within(t.Context(), time.Second, "the module", func(context.Context) (int, error) { return 7, nil }); n != 7 || err != nil {
			t.Fatalf("in time: %d, %v", n, err)
		}
		refused := errors.New("refused")
		if _, err := transient.Within(t.Context(), time.Second, "the module", func(context.Context) (int, error) { return 0, refused }); !errors.Is(err, refused) {
			t.Fatalf("its own error: %v, want refused", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := transient.Within(ctx, time.Second, "the module", waits); !errors.Is(err, context.Canceled) || errors.Is(err, transient.ErrUnavailable) {
			t.Fatalf("the caller gone: %v, want canceled", err)
		}
	})
}
