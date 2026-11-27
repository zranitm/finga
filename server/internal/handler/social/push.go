package social

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	return errors.Join(
		transport.Encode(e, func(p model.ChatMessage) any {
			return &clientv1.ChatMessage{Channel: chatChannels[p.Channel], Sender: p.Sender, SenderName: p.SenderName, Text: p.Text}
		}),
		transport.Encode(e, func(p model.PartyInvited) any {
			return &clientv1.PartyInvited{InviteId: p.InviteID, From: p.From, FromName: p.FromName}
		}),
		transport.Encode(e, func(p model.PartyState) any {
			return &clientv1.PartyState{Party: partyView(p.Party)}
		}),
	)
}

func partyView(p *model.PartyView) *clientv1.PartyView {
	if p == nil {
		return nil
	}
	v := &clientv1.PartyView{Leader: p.Leader, Members: make([]*clientv1.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, &clientv1.PartyMember{
			CharacterId: m.CharacterID, Name: m.Name, Class: m.Class, Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Channel: uint32(m.Channel), Online: m.Online,
		})
	}
	return v
}

var chatChannels = map[model.ChatChannel]clientv1.ChatChannel{
	model.ChatMap:   clientv1.ChatChannel_CHAT_CHANNEL_MAP,
	model.ChatParty: clientv1.ChatChannel_CHAT_CHANNEL_PARTY,
}
