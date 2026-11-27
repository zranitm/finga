package push

import (
	clientv1 "finga/api/gen/client/v1"
	model "finga/internal/model/gateway"
	"finga/pkg/wsconn"
)

// body is the protocol message a push reaches the client as; a Delta's reuses its frame's shared encoding.
func (o *Outbox) body(p model.Push) any {
	switch p := p.(type) {
	case model.Snapshot:
		snap := &clientv1.Snapshot{Channel: uint32(p.Channel), Area: p.Area, Players: playerViews(p.Players), Mobs: mobViews(p.Mobs), Pouches: pouchViews(p.Pouches), Boss: mobView(p.Boss)}
		snap.FieldBoss, snap.FieldBossGone = bossState(p.FieldBoss, p.FieldBossGone)
		return snap
	case model.FieldBoss:
		return fieldBoss(p)
	case model.FieldBossGone:
		return bossGone(p)
	case model.Delta:
		return wsconn.Spliced{Shared: o.frames.shared(p.Frame, p.Parts), Own: &clientv1.Delta{AckSeq: p.AckSeq, Players: ownView(p.Own), Energy: p.Energy, Unseen: unseen(p.Unseen), Pouches: pouchViews(p.Pouches), PouchesGone: instanceIDs(p.PouchesGone)}}
	case model.MapChange:
		return &clientv1.MapChange{MapId: p.MapID, X: p.X, Y: p.Y, Z: p.Z, Yaw: p.Yaw}
	case model.TeleportCancelled:
		return &clientv1.TeleportCancelled{Reason: teleportCancels[p.Reason]}
	case model.PartySpots:
		spots := make([]*clientv1.PartySpot, len(p.Spots))
		for k, s := range p.Spots {
			spots[k] = &clientv1.PartySpot{CharacterId: s.CharacterID, X: float32(s.X), Z: float32(s.Z)}
		}
		return &clientv1.PartySpots{Spots: spots}
	case model.AreaEntered:
		up, down := bossState(p.FieldBoss, p.FieldBossGone)
		return &clientv1.AreaEntered{Area: p.Area, FieldBoss: up, FieldBossGone: down}
	case model.WaypointFound:
		return &clientv1.WaypointFound{Waypoint: p.Waypoint}
	case model.PiecesAdded:
		pieces := make([]*clientv1.AddedPiece, 0, len(p.Pieces))
		for _, a := range p.Pieces {
			pieces = append(pieces, &clientv1.AddedPiece{Id: a.ID, Cause: a.Cause})
		}
		return &clientv1.PiecesAdded{Pieces: pieces}
	case model.Damage:
		return &clientv1.Damage{Source: entityRef(p.Source), Target: entityRef(p.Target), Amount: int32(p.Amount), Crit: p.Crit, Miss: p.Miss, Dodge: p.Dodge, Block: p.Block, StatusId: p.Status, Shielded: int32(p.Shielded)}
	case model.SkillUsed:
		return &clientv1.SkillUsed{Caster: p.Caster, Skill: p.Skill, Stage: int32(p.Stage), Trait: p.Trait, Aim: p.Aim, Target: lockRef(p.Target)}
	case model.AttackUsed:
		return &clientv1.AttackUsed{Attacker: p.Attacker, LengthTicks: uint32(p.LengthTicks), Aim: p.Aim} //nolint:gosec // lengths are small
	case model.MobAttacked:
		return &clientv1.MobAttacked{Mob: uint32(p.MobID), Attack: uint32(p.Attack), ToX: p.ToX, Yaw: p.Yaw, ToY: p.ToY, ToZ: p.ToZ} //nolint:gosec // small: mob IDs and attack indexes
	case model.Died:
		return &clientv1.Died{CharacterId: p.CharacterID, X: p.X, Y: p.Y, Z: p.Z}
	case model.Respawned:
		return &clientv1.Respawned{CharacterId: p.CharacterID, MapId: p.MapID, X: p.X, Y: p.Y, Hp: int32(p.HP), Z: p.Z, Yaw: p.Yaw}
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
	case model.MatchState:
		return matchState(p)
	case model.ArenaBar:
		return &clientv1.ArenaBar{SkillBar: p.Bar}
	case model.PvpQueued:
		return &clientv1.PvpQueued{Mode: p.Mode, SinceMs: uint32(p.Since.Milliseconds()), Members: p.Members, WaitingRoom: p.WaitingRoom} //nolint:gosec // a wait of minutes
	case model.PvpUnqueued:
		return &clientv1.PvpUnqueued{Reason: unqueueReasons[p.Reason], By: p.By}
	case model.PvpOffer:
		return &clientv1.PvpOffer{Offer: p.Offer, Mode: p.Mode, Bots: int32(p.Bots), UntilMs: uint32(p.Until.Milliseconds())} //nolint:gosec // a few bots, seconds
	case model.PvpOfferCancelled:
		return &clientv1.PvpOfferGone{Offer: p.Offer, Reason: goneReasons[p.Reason], Requeued: p.Requeued}
	case model.MatchEnded:
		return matchEnded(p)
	case model.Loot:
		items := make([]*clientv1.LootItem, 0, len(p.Items))
		for _, it := range p.Items {
			items = append(items, &clientv1.LootItem{Template: it.Template, Quantity: int32(it.Quantity)}) //nolint:gosec // a stack's size
		}
		return &clientv1.Loot{Kind: lootKinds[p.Kind], MapId: p.MapID, PouchId: uint32(p.Pouch), OwnerId: p.Owner, OwnerName: p.OwnerName, Items: items} //nolint:gosec // pouch IDs count up within one map instance
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

// bossState is a field boss up or beaten on the wire; nil for neither.
func bossState(up *model.FieldBoss, down *model.FieldBossGone) (*clientv1.FieldBoss, *clientv1.FieldBossGone) {
	var u *clientv1.FieldBoss
	var d *clientv1.FieldBossGone
	if up != nil {
		u = fieldBoss(*up)
	}
	if down != nil {
		d = bossGone(*down)
	}
	return u, d
}

func fieldBoss(b model.FieldBoss) *clientv1.FieldBoss {
	return &clientv1.FieldBoss{MobId: uint32(b.MobID), Type: b.Type}
}

func bossGone(b model.FieldBossGone) *clientv1.FieldBossGone {
	return &clientv1.FieldBossGone{MobId: uint32(b.MobID), Type: b.Type, BackInMs: b.BackInMs}
}

// BossList is each of a region's area bosses: up, or its time back.
func BossList(bosses []model.AreaBoss) *clientv1.BossList {
	out := &clientv1.BossList{}
	for _, b := range bosses {
		out.Bosses = append(out.Bosses, &clientv1.AreaBoss{Area: b.Area, Up: b.Up, BackInMs: b.BackInMs})
	}
	return out
}

// ChannelList is a region's open channels and how full each is.
func ChannelList(c model.Channels) *clientv1.ChannelList {
	out := &clientv1.ChannelList{Cap: uint32(c.Cap)}
	for _, l := range c.Open {
		out.Channels = append(out.Channels, &clientv1.ChannelLoad{Channel: uint32(l.Channel), Players: uint32(l.Players)})
	}
	return out
}
