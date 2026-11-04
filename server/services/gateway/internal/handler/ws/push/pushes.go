package push

import (
	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/services/gateway/internal/model"
)

// message is the protocol message a push reaches the client as.
func message(p model.Push) proto.Message {
	switch p := p.(type) {
	case model.Snapshot:
		return &clientv1.Snapshot{Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Drops: dropViews(p.Drops)}
	case model.Delta:
		return &clientv1.Delta{
			AckSeq: p.AckSeq, Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Drops: dropViews(p.Drops),
			RemovedPlayers: p.RemovedPlayers, RemovedMobs: instanceIDs(p.RemovedMobs), RemovedDrops: instanceIDs(p.RemovedDrops),
		}
	case model.MapChange:
		return &clientv1.MapChange{MapId: p.MapID, X: p.X, Y: p.Y}
	case model.Damage:
		return &clientv1.Damage{Source: entityRef(p.Source), Target: entityRef(p.Target), Amount: int32(p.Amount), Crit: p.Crit, Miss: p.Miss}
	case model.Died:
		return &clientv1.Died{CharacterId: p.CharacterID, X: p.X, Y: p.Y}
	case model.Respawned:
		return &clientv1.Respawned{CharacterId: p.CharacterID, MapId: p.MapID, X: p.X, Y: p.Y, Hp: int32(p.HP)}
	case model.ExpGained:
		return &clientv1.ExpGained{Amount: p.Amount, Exp: p.Exp}
	case model.LevelUp:
		return &clientv1.LevelUp{Character: characterView(p.Character)}
	case model.ChatMessage:
		return &clientv1.ChatMessage{Channel: chatChannels[p.Channel], Sender: p.Sender, SenderName: p.SenderName, Text: p.Text}
	case model.InventoryChanged:
		return InventoryChanged(p.Slots)
	case model.PickupFailed:
		return ErrorResponse(p.Why)
	case model.PartyState:
		return &clientv1.PartyState{Party: partyView(p.Party)}
	case model.PartyInvited:
		return &clientv1.PartyInvited{InviteId: p.InviteID, From: p.From, FromName: p.FromName}
	}
	return nil
}
