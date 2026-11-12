// Package announce sends what the maps announce to every player online, through the social service, off the tick.
package announce

import (
	"context"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/pkg/coalesce"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

const callTimeout = 2 * time.Second // one announcement to the social service

// Social is the social service as announcements need it.
type Social interface {
	Announce(ctx context.Context, a model.Announcement) error
}

// Queue holds each announcement until it is sent; a newer one of the same news replaces it, so a map never waits on it.
type Queue struct {
	social  Social
	log     *slog.Logger
	waiting *coalesce.Queue[string, model.Announcement]
}

// Deps are what the queue calls; all are required.
type Deps struct {
	Social Social
	Logger *slog.Logger
}

// New builds the queue, which sends once Run starts it.
func New(d Deps) *Queue {
	return &Queue{social: d.Social, log: d.Logger, waiting: coalesce.New(key)}
}

// Announce queues a; it never blocks.
func (q *Queue) Announce(a model.Announcement) {
	q.waiting.Put(a)
}

// Run sends the queued announcements until ctx ends. One that fails is logged and dropped: it is only news, and the map goes on without it.
func (q *Queue) Run(ctx context.Context) (err error) {
	defer panics.Capture(&err)
	q.waiting.Drain(ctx, &backoff.ZeroBackOff{}, func(a model.Announcement) coalesce.Outcome {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		err := q.social.Announce(callCtx, a)
		cancel()
		if err == nil {
			return coalesce.Handled
		}
		if ctx.Err() == nil {
			q.log.WarnContext(ctx, "an announcement failed; dropped", "announcement", key(a), "err", err)
		}
		return coalesce.Dropped
	})
	return nil
}

// key is what makes two announcements the same news: a boss appearing on the same map.
func key(a model.Announcement) string {
	switch a := a.(type) { //nolint:gocritic // a switch, so gochecksumtype names every kind added later
	case model.BossAppeared:
		return "boss_appeared:" + a.Map
	}
	return ""
}
