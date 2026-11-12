package push

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// body is the protocol message a push reaches the client as; a Delta's reuses its frame's shared encoding.
func (o *Outbox) body(p model.Push) any {
	switch p := p.(type) {
	case model.Snapshot:
		return &clientv1.Snapshot{Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Drops: dropViews(p.Drops)}
	case model.Delta:
		return wsconn.Spliced{Shared: o.frames.shared(p.Frame), Own: &clientv1.Delta{AckSeq: p.AckSeq}}
	case model.MapChange:
		return &clientv1.MapChange{MapId: p.MapID, X: p.X, Y: p.Y}
	case model.Damage:
		return &clientv1.Damage{Source: entityRef(p.Source), Target: entityRef(p.Target), Amount: int32(p.Amount), Crit: p.Crit, Miss: p.Miss}
	case model.SkillUsed:
		return &clientv1.SkillUsed{Caster: p.Caster, Skill: p.Skill, Stage: int32(p.Stage), Facing: int32(p.Facing)}
	case model.Died:
		return &clientv1.Died{CharacterId: p.CharacterID, X: p.X, Y: p.Y}
	case model.Respawned:
		return &clientv1.Respawned{CharacterId: p.CharacterID, MapId: p.MapID, X: p.X, Y: p.Y, Hp: int32(p.HP)}
	case model.ExpGained:
		return &clientv1.ExpGained{Amount: p.Amount, Exp: p.Exp}
	case model.LevelUp:
		return &clientv1.LevelUp{Character: characterView(p.Character)}
	case model.Promoted:
		return &clientv1.Promoted{Character: characterView(p.Character)}
	case model.ChatMessage:
		return &clientv1.ChatMessage{Channel: chatChannels[p.Channel], Sender: p.Sender, SenderName: p.SenderName, Text: p.Text}
	case model.InventoryChanged:
		return InventoryChanged(p)
	case model.StatsChanged:
		return statsChanged(p)
	case model.Loot:
		return &clientv1.Loot{
			Kind: lootKinds[p.Kind], MapId: p.MapID, DropId: uint32(p.DropID), OwnerId: p.Owner, OwnerName: p.OwnerName, Template: p.Template, Quantity: int32(p.Quantity),
		}
	case model.PartyState:
		return &clientv1.PartyState{Party: partyView(p.Party)}
	case model.PartyInvited:
		return &clientv1.PartyInvited{InviteId: p.InviteID, From: p.From, FromName: p.FromName}
	case model.NpcDialog:
		return npcDialog(p)
	case model.QuestChanged:
		return &clientv1.QuestChanged{Change: questChanges[p.Change], Quest: questState(p.Quest)}
	}
	return nil
}
