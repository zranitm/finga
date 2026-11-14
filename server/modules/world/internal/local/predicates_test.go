package local_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"finga/internal/kernel"
	"finga/modules/world/internal/local"
	"finga/pkg/transient"
)

// Each failure a module answers the world with, and how each of the world's checks on its calls reads it; the rows are literals, so a change to a check fails here.
func TestEachRetryPredicateReadsEachFailure(t *testing.T) {
	cause := errors.New("the module's cause")
	predicates := []struct {
		name  string
		check func(error) bool
	}{
		{"Loot.Retryable", local.Loot{}.Retryable},
		{"Saver.Retryable", local.Saver{}.Retryable},
		{"Saver.Refused", local.Saver{}.Refused},
		{"Social.Retryable", local.Social{}.Retryable},
	}
	for _, f := range []struct {
		name string
		err  error
		want [4]bool // each predicate's answer, in order
	}{
		{"a module's own failure", kernel.ErrInternal, [4]bool{true, false, false, false}},
		{"a module down or busy for now", fmt.Errorf("%w: %w", transient.ErrUnavailable, cause), [4]bool{true, true, false, true}},
		{"a rule a retry may pass", fmt.Errorf("%w: %w", kernel.ErrMapBusy, cause), [4]bool{true, true, false, true}},
		{"a final rule", fmt.Errorf("%w: %w", kernel.ErrEquipTabFull, cause), [4]bool{false, false, true, false}},
		{"a malformed ID", kernel.ErrBadID, [4]bool{false, false, true, false}},
		{"out of time", context.DeadlineExceeded, [4]bool{true, true, false, true}},
		{"given up", context.Canceled, [4]bool{true, true, false, true}},
		{"a rate limit", fmt.Errorf("%w: %w", kernel.ErrChatRateLimited, cause), [4]bool{false, false, true, false}},
		{"a session race", fmt.Errorf("%w: %w", kernel.ErrSessionRace, cause), [4]bool{false, false, true, false}},
	} {
		for k, p := range predicates {
			if got := p.check(f.err); got != f.want[k] {
				t.Errorf("%s: %s answers %v, want %v", f.name, p.name, got, f.want[k])
			}
		}
	}
}
