package social

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// A node's players go offline once its subscription has stayed gone nodeGrace; marking them takes up to sweepTimeout, and a sweep cut short tries again after sweepRetry, backing off to sweepRetryMax.
const (
	nodeGrace     = 30 * time.Second
	sweepTimeout  = 10 * time.Second
	sweepRetry    = 5 * time.Second
	sweepRetryMax = time.Minute
)

// NodeLeft says a node's subscription ended: once it has stayed gone nodeGrace, its players go offline; one back in time registers them again.
func (m *Module) NodeLeft(node string) {
	retry := &backoff.ExponentialBackOff{InitialInterval: sweepRetry, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: sweepRetryMax}
	var try func()
	try = func() {
		m.panics.Do(context.Background(), panics.FlowJob, func() {
			if err := m.sweepGone(node); err != nil && !errors.Is(err, errBack) {
				time.AfterFunc(retry.NextBackOff(), try) // a dead node never comes back to finish it
			}
		}, "job", "sweep", "node_id", node)
	}
	time.AfterFunc(nodeGrace, try)
}

// errBack ends a sweep: the server is back and registers its players again.
var errBack = errors.New("social: the server is back")

// sweepGone takes a gone node's players offline, until one fails or the server is back.
func (m *Module) sweepGone(node string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	for id, conn := range m.notes.through(node) {
		if err := m.sweep(ctx, node, id, conn); err != nil {
			return err
		}
	}
	return nil
}

// sweep takes one player of a gone node offline, unless the server is back.
func (m *Module) sweep(ctx context.Context, node string, id kernel.CharacterID, conn string) error {
	if err := m.registering.Acquire(ctx, 1); err != nil {
		return err
	}
	defer m.registering.Release(1)
	if m.notes.subscribed(node) {
		return errBack
	}
	return m.parties.Offline(ctx, id, conn)
}
