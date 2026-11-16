// Package party keeps the world's copy of the social module's parties, which a tick reads without waiting.
package party

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"golang.org/x/sync/errgroup"

	"finga/internal/kernel"
	"finga/modules/world/internal/model"
	"finga/pkg/coalesce"
	"finga/pkg/panics"
)

const (
	callTimeout = 5 * time.Second // one update to the social module

	retryFirst = 100 * time.Millisecond // the pause after a failed update, doubled by each failure in a row
	retryMax   = 5 * time.Second
)

// Social is the social module as the party copy sees it.
type Social interface {
	// FollowParties tells on each party stream event until ctx ends; a broken one tells PartiesLost, then resubscribes.
	FollowParties(ctx context.Context, world string, on func(model.PartyEvent))
	// UpdateMember tells the social module what a map saw of a member.
	UpdateMember(ctx context.Context, u model.MemberUpdate) error
	// Retryable reports whether a failed update may pass if tried again.
	Retryable(err error) bool
}

// Copy is the world's parties, kept current from the social stream so no read or update makes a tick wait.
type Copy struct {
	social  Social
	worldID string
	log     *slog.Logger
	// updates holds each member's newest update until it is sent, so none is lost to a slow social module.
	updates *coalesce.Queue[kernel.CharacterID, model.MemberUpdate]

	mu    sync.Mutex
	table map[kernel.CharacterID]model.Party
	epoch atomic.Uint64 // the party stream's subscriptions so far
}

// Deps are what the copy calls; all are required.
type Deps struct {
	Social Social
	Logger *slog.Logger
}

// Options name the node the copy follows parties for.
type Options struct {
	World string // this node's ID, which its party stream subscribes as
}

// NewCopy builds the copy, which follows social's party stream once Run starts it.
func NewCopy(d Deps, o Options) *Copy {
	return &Copy{
		social: d.Social, worldID: o.World, log: d.Logger, table: map[kernel.CharacterID]model.Party{},
		updates: coalesce.New(func(u model.MemberUpdate) kernel.CharacterID { return u.ID }),
	}
}

// Epoch counts the party stream's subscriptions: a new one means the stream broke, and parties may have changed unseen meanwhile.
func (p *Copy) Epoch() uint64 { return p.epoch.Load() }

// Party is a character's party, the zero one without.
func (p *Copy) Party(id kernel.CharacterID) model.Party {
	p.mu.Lock()
	defer p.mu.Unlock()
	party := p.table[id]
	party.Members = slices.Clone(party.Members)
	return party
}

// Update queues what a map saw of a member, replacing an older update still waiting; it never blocks.
func (p *Copy) Update(u model.MemberUpdate) {
	p.updates.Put(u)
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
		p.epoch.Add(1)
	case model.PartiesLost:
		p.reset()
		p.log.WarnContext(ctx, "party stream broke; nobody has a party until it is back", "err", e.Err)
	case model.Membership:
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, id := range e.Characters {
			if len(e.Party.Members) == 0 {
				delete(p.table, id)
				continue
			}
			p.table[id] = e.Party
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
	p.updates.Drain(ctx, wait, func(u model.MemberUpdate) coalesce.Outcome {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		err := p.social.UpdateMember(callCtx, u)
		cancel()
		switch {
		case err == nil:
			return coalesce.Handled
		case !p.social.Retryable(err):
			if ctx.Err() == nil {
				p.log.WarnContext(ctx, "member update failed", "character_id", u.ID, "err", err)
			}
			return coalesce.Dropped
		case ctx.Err() == nil:
			p.log.WarnContext(ctx, "member update failed; retrying", "character_id", u.ID, "err", err)
		}
		return coalesce.Retry
	})
}
