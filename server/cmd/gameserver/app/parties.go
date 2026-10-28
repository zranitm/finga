package app

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/model"
	"finga/pkg/grpcx"
)

// partyUpdateQueue is how many member updates may wait for the social
// service; past it they are dropped, and the member's next change carries
// the latest state anyway.
const partyUpdateQueue = 1024

// updateTimeout bounds one UpdateMember call from the worker.
const updateTimeout = 5 * time.Second

// partyCopy is the world's Parties over the social service (page 10, rule
// 2: the tick never waits on another service). The tick reads a local copy
// of party membership, fed by WatchParties and cleared when that stream
// breaks; member updates go to a bounded queue a worker sends from.
type partyCopy struct {
	c       socialv1.SocialServiceClient
	worldID string
	log     *slog.Logger
	updates chan *socialv1.UpdateMemberRequest

	mu    sync.Mutex
	table map[model.CharacterID][]model.CharacterID
}

func newPartyCopy(c socialv1.SocialServiceClient, worldID string, log *slog.Logger) *partyCopy {
	return &partyCopy{c: c, worldID: worldID, log: log, updates: make(chan *socialv1.UpdateMemberRequest, partyUpdateQueue), table: map[model.CharacterID][]model.CharacterID{}}
}

// Members lists the IDs of a character's party, nil without one.
func (p *partyCopy) Members(id model.CharacterID) []model.CharacterID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.table[id])
}

// SameParty reports whether two characters share a party.
func (p *partyCopy) SameParty(a, b model.CharacterID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.table[a], b)
}

// Update queues what the world saw of a member; it never blocks.
func (p *partyCopy) Update(id model.CharacterID, mapID string, level, hp, maxHP int) {
	select {
	case p.updates <- &socialv1.UpdateMemberRequest{CharacterId: id.String(), MapId: mapID, Level: int32(level), Hp: int32(hp), MaxHp: int32(maxHP)}:
	default:
	}
}

// Run follows WatchParties and sends the queued updates until ctx ends.
func (p *partyCopy) Run(ctx context.Context) error {
	go p.sendUpdates(ctx)
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[socialv1.WatchPartiesResponse], error) {
		return p.c.WatchParties(ctx, &socialv1.WatchPartiesRequest{WorldId: p.worldID})
	}, p.apply, grpcx.FollowOptions{OnBreak: func(err error) {
		p.clear()
		p.log.WarnContext(ctx, "party stream broke; nobody has a party until it is back", "err", err)
	}})
	return nil
}

func (p *partyCopy) apply(m *socialv1.WatchPartiesResponse) {
	if m.GetSubscribed() != nil {
		p.clear() // the memberships that follow are the whole table
		return
	}
	ms := m.GetMembership()
	members := make([]model.CharacterID, 0, len(ms.GetMembers()))
	for _, s := range ms.GetMembers() {
		if id, err := model.ParseCharacterID(s); err == nil {
			members = append(members, id)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range ms.GetCharacterIds() {
		id, err := model.ParseCharacterID(s)
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

func (p *partyCopy) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.table)
}

func (p *partyCopy) sendUpdates(ctx context.Context) {
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
