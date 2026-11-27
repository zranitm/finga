package social

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/social"
	"finga/internal/handler/transport"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	return errors.Join(
		transport.Encode(e, func(p social.ChatLine) any { return chatLine(p) }),
		transport.Encode(e, func(p social.Invited) any { return invited(p) }),
		transport.Encode(e, func(p social.PartyState) any { return partyState(p) }),
	)
}

// chatLine is a party's line: the social module delivers those, and the world a map's.
func chatLine(p social.ChatLine) *clientv1.ChatMessage {
	return &clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: p.Sender.String(), SenderName: p.SenderName, Text: p.Text}
}

func invited(p social.Invited) *clientv1.PartyInvited {
	return &clientv1.PartyInvited{InviteId: p.Invite.String(), From: p.From.String(), FromName: p.FromName}
}

// partyState is the player's party, none when they have none.
func partyState(p social.PartyState) *clientv1.PartyState {
	return &clientv1.PartyState{Party: partyView(p.Party)}
}

func partyView(p *social.Party) *clientv1.PartyView {
	if p == nil {
		return nil
	}
	v := &clientv1.PartyView{Leader: p.Leader.String(), Members: make([]*clientv1.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, &clientv1.PartyMember{
			CharacterId: m.ID.String(), Name: m.Name, Class: m.Class.String(), Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Channel: uint32(m.Channel), Online: m.Online, //nolint:gosec // small
		})
	}
	return v
}
