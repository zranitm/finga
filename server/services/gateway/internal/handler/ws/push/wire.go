package push

import (
	"strconv"

	clientv1 "finga/api/gen/client/v1"
	"finga/services/gateway/internal/model"
)

// InventoryChanged is the message for slots an inventory command or a pickup changed.
func InventoryChanged(slots []model.InventorySlot) *clientv1.InventoryChanged {
	out := make([]*clientv1.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := &clientv1.InventorySlot{Tab: s.Tab, Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = itemView(*s.Item)
		}
		out = append(out, slot)
	}
	return &clientv1.InventoryChanged{Slots: out}
}

func characterView(c model.CharacterView) *clientv1.CharacterView {
	st := c.Stats
	return &clientv1.CharacterView{
		Id: c.ID, Name: c.Name, Class: c.Class, Level: int32(c.Level), Exp: c.Exp,
		Stats: &clientv1.Stats{Str: int32(st.STR), Dex: int32(st.DEX), Int: int32(st.INT), Agi: int32(st.AGI), Vit: int32(st.VIT)},
		Hp:    int32(c.HP), MaxHp: int32(c.MaxHP), Mp: int32(c.MP), MaxMp: int32(c.MaxMP),
	}
}

func playerViews(ps []model.PlayerView) []*clientv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &clientv1.PlayerView{
			Id: p.ID, Name: p.Name, Class: p.Class, Level: int32(p.Level),
			X: p.X, Y: p.Y, Vx: p.VX, Vy: p.VY, Facing: int32(p.Facing),
			OnGround: p.OnGround, Climbing: p.Climbing, Hp: int32(p.HP), MaxHp: int32(p.MaxHP), Dead: p.Dead,
			Invulnerable: p.Invulnerable,
		})
	}
	return out
}

func mobViews(ms []model.MobView) []*clientv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*clientv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &clientv1.MobView{
			Id: uint32(m.ID), Type: m.Type, X: m.X, Y: m.Y, Facing: int32(m.Facing), Hp: int32(m.HP), MaxHp: int32(m.MaxHP),
		})
	}
	return out
}

func dropViews(ds []model.DropView) []*clientv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*clientv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &clientv1.DropView{Id: uint32(d.ID), Template: d.Template, Quantity: int32(d.Quantity), X: d.X, Y: d.Y})
	}
	return out
}

// instanceIDs are mob or drop IDs, which count up within one map instance and fit the wire's 32 bits.
func instanceIDs(ids []uint64) []uint32 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		out = append(out, uint32(id))
	}
	return out
}

func itemViews(items []model.ItemView) []*clientv1.ItemView {
	out := make([]*clientv1.ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, itemView(it))
	}
	return out
}

func itemView(it model.ItemView) *clientv1.ItemView {
	return &clientv1.ItemView{ItemId: it.ID, Template: it.Template, Tab: it.Tab, Slot: int32(it.Slot), Quantity: int32(it.Quantity)}
}

func partyView(p *model.PartyView) *clientv1.PartyView {
	if p == nil {
		return nil
	}
	v := &clientv1.PartyView{Leader: p.Leader, Members: make([]*clientv1.PartyMember, 0, len(p.Members))}
	for _, m := range p.Members {
		v.Members = append(v.Members, &clientv1.PartyMember{
			CharacterId: m.CharacterID, Name: m.Name, Level: int32(m.Level), Hp: int32(m.HP), MaxHp: int32(m.MaxHP), MapId: m.MapID, Online: m.Online,
		})
	}
	return v
}

var chatChannels = map[model.ChatChannel]clientv1.ChatChannel{
	model.ChatMap:   clientv1.ChatChannel_CHAT_CHANNEL_MAP,
	model.ChatParty: clientv1.ChatChannel_CHAT_CHANNEL_PARTY,
}

// entityRef is how the client names a player or a mob: p:<character id> or m:<mob id>.
func entityRef(e model.Entity) string {
	if e.Character != "" {
		return "p:" + e.Character
	}
	return "m:" + strconv.FormatUint(e.Mob, 10)
}
