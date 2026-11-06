// Package party keeps the world's copy of the social service's parties, which a tick reads without waiting.
package party

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

const (
	// updateQueue bounds the member updates to send; a full queue drops one, as the member's next carries the latest.
	updateQueue = 1024
	callTimeout = 5 * time.Second // one update to the social service
)

// Social is the social service as the party copy sees it.
type Social interface {
	// FollowParties tells on each party stream event until ctx ends; a broken one tells PartiesLost, then resubscribes.
	FollowParties(ctx context.Context, world string, on func(model.PartyEvent))
	// UpdateMember tells the service what a map saw of a member.
	UpdateMember(ctx context.Context, u model.MemberUpdate) error
}

// Copy is the world's parties, kept current from the social stream so no read or update makes a tick wait.
type Copy struct {
	social  Social
	worldID string
	log     *slog.Logger
	updates chan model.MemberUpdate

	mu    sync.Mutex
	table map[kernel.CharacterID][]kernel.CharacterID
}

// NewCopy builds the copy, which follows social's party stream as worldID once Run starts it.
func NewCopy(social Social, worldID string, log *slog.Logger) *Copy {
	return &Copy{social: social, worldID: worldID, log: log, updates: make(chan model.MemberUpdate, updateQueue), table: map[kernel.CharacterID][]kernel.CharacterID{}}
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

// Update queues what a map saw of a member; it never blocks.
func (p *Copy) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	select {
	case p.updates <- model.MemberUpdate{ID: id, MapID: mapID, Level: level, HP: hp, MaxHP: maxHP}:
	default:
	}
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

func (p *Copy) sendUpdates(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case u := <-p.updates:
			callCtx, cancel := context.WithTimeout(ctx, callTimeout)
			if err := p.social.UpdateMember(callCtx, u); err != nil && ctx.Err() == nil {
				p.log.WarnContext(ctx, "member update failed", "character_id", u.ID, "err", err)
			}
			cancel()
		}
	}
}
