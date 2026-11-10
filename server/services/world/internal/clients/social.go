package clients

import (
	"context"

	"google.golang.org/grpc"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/panics"
	"finga/services/world/internal/model"
)

// Social adapts the social service to party.Social.
type Social struct {
	c      socialv1.SocialServiceClient
	panics *panics.Recorder
}

// NewSocial adapts the social client; rec records a panic handling the party stream.
func NewSocial(c socialv1.SocialServiceClient, rec *panics.Recorder) Social {
	return Social{c: c, panics: rec}
}

// FollowParties follows WatchParties as world until ctx ends, subscribing again after a break.
func (s Social) FollowParties(ctx context.Context, world string, on func(model.PartyEvent)) {
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[socialv1.WatchPartiesResponse], error) {
		return s.c.WatchParties(ctx, &socialv1.WatchPartiesRequest{WorldId: world})
	}, func(m *socialv1.WatchPartiesResponse) {
		if m.GetSubscribed() != nil {
			on(model.PartiesSubscribed{})
			return
		}
		if ms := m.GetMembership(); ms != nil {
			on(model.Membership{Characters: characterIDs(ms.GetCharacterIds()), Party: model.Party{ID: ms.GetPartyId(), Members: characterIDs(ms.GetMembers())}})
		}
	}, grpcx.FollowOptions{Panics: s.panics, OnBreak: func(err error) { on(model.PartiesLost{Err: err}) }})
}

// UpdateMember tells the service what a map saw of a member.
func (s Social) UpdateMember(ctx context.Context, u model.MemberUpdate) error {
	_, err := s.c.UpdateMember(ctx, &socialv1.UpdateMemberRequest{CharacterId: u.ID.String(), MapId: u.MapID, Level: int32(u.Level), Hp: int32(u.HP), MaxHp: int32(u.MaxHP)})
	return err
}

// Retryable is a failure the social service may yet get past; any other would be retried forever.
func (Social) Retryable(err error) bool { return grpcx.IsTransient(err) }

// characterIDs parses ids, leaving out any that is not one.
func characterIDs(ids []string) []kernel.CharacterID {
	out := make([]kernel.CharacterID, 0, len(ids))
	for _, s := range ids {
		if id, err := kernel.ParseCharacterID(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}
