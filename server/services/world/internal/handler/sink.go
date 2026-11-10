package handler

import (
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/pkg/hub"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
)

// streamSink queues a player's events on its gateway's stream; false drops the player.
type streamSink struct {
	sub       *hub.Sub[*worldv1.DeliverResponse]
	character string
	conn      string
}

// Stream is the player's gateway's stream, which every player joined through that gateway shares.
func (s *streamSink) Stream() instance.Stream { return gatewayStream{sub: s.sub} }

// Send turns one world event into a delivery and queues it.
func (s *streamSink) Send(e model.Event) bool {
	m := s.delivery(e)
	m.CharacterId, m.ConnectionId = s.character, s.conn
	return s.sub.Send(m)
}

func (s *streamSink) delivery(e model.Event) *worldv1.DeliverResponse {
	switch ev := e.(type) {
	case model.Snapshot:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{
			MapId: ev.MapID, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops),
		}}}
	case model.Teleport:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: ev.MapID, X: ev.X, Y: ev.Y}}}
	case model.Damage:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{
			Source: entityRef(ev.Source), Target: entityRef(ev.Target), Amount: int32(ev.Amount), Crit: ev.Crit, Miss: ev.Miss,
		}}}
	case model.Died:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Died{Died: &worldv1.Died{CharacterId: ev.ID.String(), X: ev.X, Y: ev.Y}}}
	case model.Respawned:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Respawned{Respawned: &worldv1.Respawned{
			CharacterId: ev.ID.String(), MapId: ev.MapID, X: ev.X, Y: ev.Y, Hp: int32(ev.HP),
		}}}
	case model.ExpGained:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_ExpGained{ExpGained: &worldv1.ExpGained{Amount: ev.Amount, Exp: ev.Exp}}}
	case model.LevelUp:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_LevelUp{LevelUp: &worldv1.LevelUp{Character: &worldv1.CharacterView{
			Id: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: int32(ev.Level), Exp: ev.Exp, Stats: stats(ev.Stats),
			Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP), Mp: int32(ev.MP), MaxMp: int32(ev.MaxMP), Sheet: statSheet(ev.Sheet),
		}}}}
	case model.StatsChanged:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
			Sheet: statSheet(ev.Sheet), Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP), Mp: int32(ev.MP), MaxMp: int32(ev.MaxMP),
		}}}
	case model.Chat:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Chat{Chat: &worldv1.Chat{SenderId: ev.Sender.String(), SenderName: ev.SenderName, Text: ev.Text}}}
	case model.InventoryChanged:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{
			Slots: slots(ev.Slots), Revision: ev.Revision, Equipment: equipSlots(ev.Equipment),
		}}}
	case model.Loot:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Loot{Loot: &worldv1.Loot{
			Kind: lootKind(ev.Kind), MapId: ev.MapID, DropId: ev.DropID, OwnerId: ev.Owner.String(), OwnerName: ev.OwnerName, Template: ev.Template, Quantity: int32(ev.Quantity),
		}}}
	case model.Detached:
		return &worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Detached{Detached: &worldv1.Detached{}}}
	}
	return nil
}

func lootKind(k model.LootKind) worldv1.LootKind {
	switch k {
	case model.LootCollected:
		return worldv1.LootKind_LOOT_KIND_COLLECTED
	case model.LootWaiting:
		return worldv1.LootKind_LOOT_KIND_WAITING
	case model.LootLost:
		return worldv1.LootKind_LOOT_KIND_LOST
	}
	return worldv1.LootKind_LOOT_KIND_UNSPECIFIED
}

func entityRef(r model.EntityRef) *worldv1.EntityRef {
	if r.IsMob() {
		return &worldv1.EntityRef{Ref: &worldv1.EntityRef_MobId{MobId: r.Mob}}
	}
	return &worldv1.EntityRef{Ref: &worldv1.EntityRef_CharacterId{CharacterId: r.Player.String()}}
}

func stats(s gamedata.Stats) *worldv1.Stats {
	return &worldv1.Stats{Strength: int32(s.STR), Dexterity: int32(s.DEX), Intelligence: int32(s.INT), Agility: int32(s.AGI), Vitality: int32(s.VIT)}
}

func playerViews(ps []model.PlayerView) []*worldv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*worldv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &worldv1.PlayerView{
			Id: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: int32(p.Level), Hp: int32(p.HP), MaxHp: int32(p.MaxHP),
			Dead: p.Dead, Invulnerable: p.Invulnerable,
			X: p.State.X, Y: p.State.Y, Vx: p.State.VX, Vy: p.State.VY, Facing: int32(p.State.Facing),
			OnGround: p.State.OnGround, Climbing: p.State.Climbing,
		})
	}
	return out
}

func mobViews(ms []model.MobView) []*worldv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*worldv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &worldv1.MobView{Id: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: int32(m.Facing), Hp: int32(m.HP), MaxHp: int32(m.MaxHP)})
	}
	return out
}

func dropViews(ds []model.DropView) []*worldv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*worldv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &worldv1.DropView{Id: d.ID, Template: d.Template, Quantity: int32(d.Quantity), X: d.X, Y: d.Y, OwnerId: d.Owner.String()})
	}
	return out
}

func slots(in []model.Slot) []*worldv1.Slot {
	out := make([]*worldv1.Slot, 0, len(in))
	for _, s := range in {
		slot := &worldv1.Slot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = item(s.Item)
			slot.Item.Tab, slot.Item.Slot = string(s.Tab), int32(s.Slot)
		}
		out = append(out, slot)
	}
	return out
}

func equipSlots(in []model.EquipSlot) []*worldv1.EquipSlot {
	if len(in) == 0 {
		return nil
	}
	out := make([]*worldv1.EquipSlot, 0, len(in))
	for _, s := range in {
		slot := &worldv1.EquipSlot{EquipSlot: s.Slot}
		if s.Item != nil {
			slot.Item = item(s.Item)
		}
		out = append(out, slot)
	}
	return out
}

func item(it *model.SlotItem) *worldv1.Item {
	out := &worldv1.Item{Id: it.ID.String(), TemplateId: it.Template, Quantity: int32(it.Quantity)}
	if s := it.Stats; s != nil {
		//nolint:gosec // stats are small
		out.Stats = &worldv1.ItemStats{
			Attack: int32(s.Attack), Defense: int32(s.Defense), Str: int32(s.STR), Dex: int32(s.DEX), Int: int32(s.INT), Agi: int32(s.AGI), Vit: int32(s.VIT),
			Accuracy: int32(s.Accuracy), Evasion: int32(s.Evasion), CritRate: int32(s.CritRate), CritDamage: int32(s.CritDamage), MaxHp: int32(s.MaxHP), MaxMp: int32(s.MaxMP),
		}
	}
	return out
}

func statSheet(s progression.Sheet) *worldv1.StatSheet {
	return &worldv1.StatSheet{Primary: stats(s.Primary), PrimaryWorn: stats(s.PrimaryWorn), Derived: derived(s.Derived), DerivedWorn: derived(s.DerivedWorn)}
}

func derived(d progression.Derived) *worldv1.DerivedStats {
	return &worldv1.DerivedStats{
		AttackPower: d.AttackPower, Defense: d.Defense, Accuracy: d.Accuracy, Evasion: d.Evasion, CritRate: d.CritRate, CritDamage: d.CritDamage,
		MaxHp: int32(d.MaxHP), MaxMp: int32(d.MaxMP), //nolint:gosec // HP and MP are small
	}
}

// gatewayStream is one gateway's delivery stream.
type gatewayStream struct {
	sub *hub.Sub[*worldv1.DeliverResponse]
}

// SendFrame queues the frame once for all its recipients on the gateway.
func (g gatewayStream) SendFrame(f *model.Frame, to []model.Recipient) bool {
	recipients := make([]*worldv1.Recipient, 0, len(to))
	for _, r := range to {
		recipients = append(recipients, &worldv1.Recipient{CharacterId: r.Character.String(), ConnectionId: r.Connection, AckSeq: r.AckSeq})
	}
	return g.sub.Send(&worldv1.DeliverResponse{Tick: f.Tick, Event: &worldv1.DeliverResponse_Frame{Frame: &worldv1.Frame{Delta: encodeFrame(f), Recipients: recipients}}})
}

// encodeFrame is the frame as a Delta without an AckSeq; each recipient has its own.
func encodeFrame(f *model.Frame) *worldv1.Delta {
	d := &worldv1.Delta{
		Players: playerViews(f.Players), Mobs: mobViews(f.Mobs), Drops: dropViews(f.Drops),
		RemovedMobs: f.RemovedMobs, RemovedDrops: f.RemovedDrops, CollectedDrops: f.CollectedDrops,
	}
	for _, id := range f.Removed {
		d.RemovedPlayers = append(d.RemovedPlayers, id.String())
	}
	return d
}
