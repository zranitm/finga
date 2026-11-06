// Package party keeps the world's copy of the social service's parties, which a tick reads without waiting.
package party

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/errgroup"

	"finga/internal/kernel"
	"finga/pkg/coalesce"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

const (
	callTimeout = 5 * time.Second // one update to the social service

	retryFirst = 100 * time.Millisecond // the pause after a failed update, doubled by each failure in a row
	retryMax   = 5 * time.Second
)

// Social is the social service as the party copy sees it.
type Social interface {
	// FollowParties tells on each party stream event until ctx ends; a broken one tells PartiesLost, then resubscribes.
	FollowParties(ctx context.Context, world string, on func(model.PartyEvent))
	// UpdateMember tells the service what a map saw of a member.
	UpdateMember(ctx context.Context, u model.MemberUpdate) error
	// Retryable reports whether a failed update may pass if tried again.
	Retryable(err error) bool
}

// Copy is the world's parties, kept current from the social stream so no read or update makes a tick wait.
type Copy struct {
	social  Social
	worldID string
	log     *slog.Logger
	// updates holds each member's newest update until it is sent, so none is lost to a slow social service.
	updates *coalesce.Queue[kernel.CharacterID, model.MemberUpdate]

	mu    sync.Mutex
	table map[kernel.CharacterID][]kernel.CharacterID
}

// NewCopy builds the copy, which follows social's party stream as worldID once Run starts it.
func NewCopy(social Social, worldID string, log *slog.Logger) *Copy {
	return &Copy{
		social: social, worldID: worldID, log: log, table: map[kernel.CharacterID][]kernel.CharacterID{},
		updates: coalesce.New(func(u model.MemberUpdate) kernel.CharacterID { return u.ID }),
	}
}

// Members lists the IDs of a character's party, nil without one.
func (p *Copy) Members(id kernel.CharacterID) []kernel.CharacterID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.table[id])
}

// SameParty reports whether two characters share a party.
func (p *Copy) SameParty(a, b kernel.CharacterID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.table[a], b)
}

// Update queues what a map saw of a member, replacing an older update still waiting; it never blocks.
func (p *Copy) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	p.updates.Put(model.MemberUpdate{ID: id, MapID: mapID, Level: level, HP: hp, MaxHP: maxHP})
}

// Run follows the party stream and sends the queued updates until ctx ends or either panics.
func (p *Copy) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		defer panics.Capture(&err)
		p.sendUpdates(gctx)
		return nil
	})
	g.Go(func() (err error) {
		defer panics.Capture(&err)
		p.social.FollowParties(gctx, p.worldID, func(e model.PartyEvent) { p.apply(gctx, e) })
		return nil
	})
	return g.Wait()
}

func (p *Copy) apply(ctx context.Context, e model.PartyEvent) {
	switch e := e.(type) {
	case model.PartiesSubscribed:
		p.reset() // the memberships that follow are the whole table
	case model.PartiesLost:
		p.reset()
		p.log.WarnContext(ctx, "party stream broke; nobody has a party until it is back", "err", e.Err)
	case model.Membership:
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, id := range e.Characters {
			if len(e.Members) == 0 {
				delete(p.table, id)
				continue
			}
			p.table[id] = e.Members
		}
	}
}

func (p *Copy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.table)
}

// sendUpdates sends each member's newest update in turn; one that may pass is retried behind the others, after a pause.
func (p *Copy) sendUpdates(ctx context.Context) {
	wait := &backoff.ExponentialBackOff{InitialInterval: retryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: retryMax}
	for ctx.Err() == nil {
		u, ok := p.updates.Take()
		if !ok {
			select {
			case <-p.updates.Ready():
			case <-ctx.Done():
			}
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		err := p.social.UpdateMember(callCtx, u)
		cancel()
		retry := err != nil && p.social.Retryable(err)
		p.updates.Done(u, retry)
		switch {
		case err == nil:
			wait.Reset()
		case ctx.Err() != nil:
		case !retry:
			p.log.WarnContext(ctx, "member update failed", "character_id", u.ID, "err", err)
		default:
			p.log.WarnContext(ctx, "member update failed; retrying", "character_id", u.ID, "err", err)
			pause(ctx, wait.NextBackOff())
		}
	}
}

// pause waits d, or until ctx ends.
func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
