package clients

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/panics"
)

// partyUpdateQueue overflow drops updates; the member's next change carries the latest state.
const partyUpdateQueue = 1024

const updateTimeout = 5 * time.Second

// PartyCopy is the world's Parties: a local copy fed by WatchParties, so the tick never waits.
type PartyCopy struct {
	c       socialv1.SocialServiceClient
	worldID string
	log     *slog.Logger
	panics  *panics.Recorder
	updates chan *socialv1.UpdateMemberRequest

	mu    sync.Mutex
	table map[kernel.CharacterID][]kernel.CharacterID
}

// NewPartyCopy adapts the social client; Run feeds the copy, and rec records a panic handling the stream.
func NewPartyCopy(c socialv1.SocialServiceClient, worldID string, log *slog.Logger, rec *panics.Recorder) *PartyCopy {
	return &PartyCopy{c: c, worldID: worldID, log: log, panics: rec, updates: make(chan *socialv1.UpdateMemberRequest, partyUpdateQueue), table: map[kernel.CharacterID][]kernel.CharacterID{}}
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

// Update queues what the world saw of a member; it never blocks.
func (p *PartyCopy) Update(id kernel.CharacterID, mapID string, level, hp, maxHP int) {
	select {
	case p.updates <- &socialv1.UpdateMemberRequest{CharacterId: id.String(), MapId: mapID, Level: int32(level), Hp: int32(hp), MaxHp: int32(maxHP)}:
	default:
	}
}

// Run follows WatchParties and sends the queued updates until ctx ends or either panics.
func (p *PartyCopy) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		defer panics.Capture(&err)
		p.sendUpdates(gctx)
		return nil
	})
	g.Go(func() (err error) {
		defer panics.Capture(&err)
		grpcx.Follow(gctx, func(ctx context.Context) (grpc.ServerStreamingClient[socialv1.WatchPartiesResponse], error) {
			return p.c.WatchParties(ctx, &socialv1.WatchPartiesRequest{WorldId: p.worldID})
		}, p.apply, grpcx.FollowOptions{Panics: p.panics, OnBreak: func(err error) {
			p.clear()
			p.log.WarnContext(gctx, "party stream broke; nobody has a party until it is back", "err", err)
		}})
		return nil
	})
	return g.Wait()
}

func (p *PartyCopy) apply(m *socialv1.WatchPartiesResponse) {
	if m.GetSubscribed() != nil {
		p.clear() // the memberships that follow are the whole table
		return
	}
	ms := m.GetMembership()
	members := make([]kernel.CharacterID, 0, len(ms.GetMembers()))
	for _, s := range ms.GetMembers() {
		if id, err := kernel.ParseCharacterID(s); err == nil {
			members = append(members, id)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range ms.GetCharacterIds() {
		id, err := kernel.ParseCharacterID(s)
		if err != nil {
			continue
		}
		if len(members) == 0 {
			delete(p.table, id)
			continue
		}
		p.table[id] = members
	}
}

func (p *PartyCopy) clear() {
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
			callCtx, cancel := context.WithTimeout(ctx, updateTimeout)
			if _, err := p.c.UpdateMember(callCtx, u); err != nil && ctx.Err() == nil {
				p.log.WarnContext(ctx, "member update failed", "character_id", u.GetCharacterId(), "err", err)
			}
			cancel()
		}
	}
}
