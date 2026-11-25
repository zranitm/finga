package push

import (
	"context"
	"time"

	model "finga/internal/model/gateway"
	"finga/pkg/panics"
)

const (
	toldQueue   = 64              // outcomes waiting to be marked told; one past it stays untold, and the next Welcome tells it again
	toldTimeout = 5 * time.Second // one mark's call
)

// pvpDelivered hands a pvp push to its player, if they're on this gateway; an outcome they were away for is then marked told, off the stream.
func (g *Gateway) pvpDelivered(ev model.PvpDelivery) {
	s := g.sessions.ByCharacter(ev.Character)
	if s == nil {
		return
	}
	s.DeliverSocial(ev.Delivery)
	if !ev.Untold.IsZero() {
		select {
		case g.told <- ev:
		default:
		}
	}
}

// markTold marks each outcome handed on live told, so the player's next Welcome doesn't tell it again, until ctx ends; a mark that fails leaves it to tell then.
func (g *Gateway) markTold(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-g.told:
			g.deps.Panics.Do(ctx, panics.FlowJob, func() {
				ctx, cancel := context.WithTimeout(ctx, toldTimeout)
				defer cancel()
				if err := g.deps.Pvp.Told(ctx, ev.Character, ev.Untold); err != nil {
					g.deps.Logger.WarnContext(ctx, "an outcome handed on live stays untold", "character", ev.Character, "match", ev.Untold, "err", err)
				}
			}, "job", "mark_told")
		}
	}
}
