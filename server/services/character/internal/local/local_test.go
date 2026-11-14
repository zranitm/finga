package local_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/character/internal/local"
)

// Each failure the item module answers a starter grant with, and whether character creation retries it; the rows are literals, so a change to the check fails here.
func TestTheStarterGrantRetriesAllButAFinalRule(t *testing.T) {
	cause := errors.New("the module's cause")
	for _, f := range []struct {
		name  string
		err   error
		retry bool
	}{
		{"a module's own failure", kernel.ErrInternal, true},
		{"a module down or busy for now", fmt.Errorf("%w: %w", transient.ErrUnavailable, cause), true},
		{"a rule a retry may pass", fmt.Errorf("%w: %w", kernel.ErrMapBusy, cause), true},
		{"a final rule", fmt.Errorf("%w: %w", kernel.ErrUnknownItem, cause), false},
		{"a malformed ID", kernel.ErrBadID, false},
		{"out of time", context.DeadlineExceeded, true},
		{"given up", context.Canceled, true},
		{"a rate limit", fmt.Errorf("%w: %w", kernel.ErrChatRateLimited, cause), false},
		{"a session race", fmt.Errorf("%w: %w", kernel.ErrSessionRace, cause), false},
	} {
		if got := (local.Starter{}).Retryable(f.err); got != f.retry {
			t.Errorf("%s: retried %v, want %v", f.name, got, f.retry)
		}
	}
}
