package application

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

// partyUpdateQueue bounds the member updates waiting to be sent; a full queue drops one, as the
// member's next change carries the latest state.
const partyUpdateQueue = 1024

// PartyCopy is the world's Parties: its copy of party membership, which the social service's stream
// keeps current, so a tick reads it without waiting. It also sends what the maps see of members, from a
// queue no tick waits on.
type PartyCopy struct {
	social  Social
	worldID string
	log     *slog.Logger
	updates chan model.MemberUpdate

	mu    sync.Mutex
	table map[kernel.CharacterID][]kernel.CharacterID
}

// NewPartyCopy builds the copy, which follows social's party stream as worldID once Run starts it.
func NewPartyCopy(social Social, worldID string, log *slog.Logger) *PartyCopy {
	return &PartyCopy{social: social, worldID: worldID, log: log, updates: make(chan model.MemberUpdate, partyUpdateQueue), table: map[kernel.CharacterID][]kernel.CharacterID{}}
}

// Members lists the IDs of a character's party, nil without one.
func (p *PartyCopy) Members(id kernel.CharacterID) []kernel.CharacterID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.table[id])
}

// SameParty reports whether two characters share a party.
func (p *PartyCopy) SameParty(a, b kernel.CharacterID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.table[a], b)
}

// Update queues what a map saw of a member; it never blocks.
func (p *PartyCopy) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	select {
	case p.updates <- model.MemberUpdate{ID: id, MapID: mapID, Level: level, HP: hp, MaxHP: maxHP}:
	default:
	}
}

// Run follows the party stream and sends the queued updates until ctx ends or either panics.
func (p *PartyCopy) Run(ctx context.Context) error {
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

func (p *PartyCopy) apply(ctx context.Context, e model.PartyEvent) {
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

func (p *PartyCopy) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.table)
}

func (p *PartyCopy) sendUpdates(ctx context.Context) {
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
