package social

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/internal/kernel"
	"finga/pkg/panics"
)

// A gateway's players go offline once its subscription has stayed gone gatewayGrace; marking them takes up to sweepTimeout, and a sweep cut short tries again after sweepRetry, backing off to sweepRetryMax.
const (
	gatewayGrace  = 30 * time.Second
	sweepTimeout  = 10 * time.Second
	sweepRetry    = 5 * time.Second
	sweepRetryMax = time.Minute
)

// GatewayLeft says a gateway's subscription ended: once it has stayed gone gatewayGrace, its players go offline; one back in time registers them again.
func (m *Module) GatewayLeft(gateway string) {
	retry := &backoff.ExponentialBackOff{InitialInterval: sweepRetry, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: sweepRetryMax}
	var try func()
	try = func() {
		m.panics.Do(context.Background(), panics.FlowJob, func() {
			if err := m.sweepGone(gateway); err != nil && !errors.Is(err, errBack) {
				time.AfterFunc(retry.NextBackOff(), try) // a dead gateway never comes back to finish it
			}
		}, "job", "sweep", "gateway_id", gateway)
	}
	time.AfterFunc(gatewayGrace, try)
}

// errBack ends a sweep: the gateway is back and registers its players again.
var errBack = errors.New("social: the gateway is back")

// sweepGone takes a gone gateway's players offline, until one fails or the gateway is back.
func (m *Module) sweepGone(gateway string) error {
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	for id, conn := range m.notes.through(gateway) {
		if err := m.sweep(ctx, gateway, id, conn); err != nil {
			return err
		}
	}
	return nil
}

// sweep takes one player of a gone gateway offline, unless the gateway is back.
func (m *Module) sweep(ctx context.Context, gateway string, id kernel.CharacterID, conn string) error {
	if err := m.registering.Acquire(ctx, 1); err != nil {
		return err
	}
	defer m.registering.Release(1)
	if m.notes.subscribed(gateway) {
		return errBack
	}
	return m.parties.Offline(ctx, id, conn)
}
