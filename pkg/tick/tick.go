// Package tick runs a fixed-timestep loop: it calls a Steppable once per
// interval, numbers the ticks, and reports how long each step took so the
// caller can count overruns. A step that runs past the interval delays the
// next one; missed intervals are dropped, not replayed.
package tick

import (
	"context"
	"errors"
	"time"
)

// Steppable is what the loop drives.
type Steppable interface {
	// Step runs one tick. dt is the interval, the simulated time the step
	// covers, whatever the wall clock did.
	Step(tick uint64, dt time.Duration)
}

// Observer sees each completed step: its number, how long it took, and
// the interval it had.
type Observer func(tick uint64, took, interval time.Duration)

// Option tunes Run.
type Option func(*config)

type config struct {
	observer Observer
}

// WithObserver reports every step.
func WithObserver(o Observer) Option {
	return func(c *config) { c.observer = o }
}

// ErrInterval is returned for a non-positive interval.
var ErrInterval = errors.New("tick: interval must be positive")

// Run steps s every interval until ctx is done, then returns nil.
func Run(ctx context.Context, interval time.Duration, s Steppable, opts ...Option) error {
	if interval <= 0 {
		return ErrInterval
	}
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var n uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		n++
		start := time.Now()
		s.Step(n, interval)
		if cfg.observer != nil {
			cfg.observer(n, time.Since(start), interval)
		}
	}
}
