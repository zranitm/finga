package repository

import (
	"encoding/json"
	"fmt"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// reply is a Result as idempotency_keys stores it; its JSON names are stored, so never rename or remove one, only add.
type reply struct {
	Changed   []replySlot     `json:"changed"`
	Revision  uint64          `json:"revision,omitempty"`
	Equipment []replyWornSlot `json:"equipment,omitempty"`
	Worn      []replyItem     `json:"worn,omitempty"`
}

type replySlot struct {
	Tab  string     `json:"tab"`
	Slot int        `json:"slot"`
	Item *replyItem `json:"item,omitempty"`
}

type replyWornSlot struct {
	Slot string     `json:"slot"`
	Item *replyItem `json:"item,omitempty"`
}

type replyItem struct {
	ID         string      `json:"ID"`
	TemplateID string      `json:"TemplateID"`
	Owner      string      `json:"Owner"`
	Tab        string      `json:"Tab"`
	Slot       int         `json:"Slot"`
	Quantity   int         `json:"Quantity"`
	Stats      *replyStats `json:"Stats,omitempty"`
	EquipSlot  string      `json:"EquipSlot,omitempty"`
}

// replyStats is a roll; a stat at 0 is left out.
type replyStats struct {
	Attack     int `json:"Attack,omitempty"`
	Defense    int `json:"Defense,omitempty"`
	STR        int `json:"STR,omitempty"`
	DEX        int `json:"DEX,omitempty"`
	INT        int `json:"INT,omitempty"`
	AGI        int `json:"AGI,omitempty"`
	VIT        int `json:"VIT,omitempty"`
	Accuracy   int `json:"Accuracy,omitempty"`
	Evasion    int `json:"Evasion,omitempty"`
	CritRate   int `json:"CritRate,omitempty"`
	CritDamage int `json:"CritDamage,omitempty"`
	MaxHP      int `json:"MaxHP,omitempty"`
	MaxMP      int `json:"MaxMP,omitempty"`
}

func encodeReply(res application.Result) ([]byte, error) {
	r := reply{Revision: res.Revision}
	if res.Changed != nil {
		r.Changed = make([]replySlot, 0, len(res.Changed))
	}
	for _, s := range res.Changed {
		r.Changed = append(r.Changed, replySlot{Tab: string(s.Tab), Slot: s.Slot, Item: encodeItem(s.Item)})
	}
	for _, s := range res.Equipment {
		r.Equipment = append(r.Equipment, replyWornSlot{Slot: s.Slot, Item: encodeItem(s.Item)})
	}
	for _, it := range res.Worn {
		r.Worn = append(r.Worn, *encodeItem(&it))
	}
	return json.Marshal(r)
}

func encodeItem(it *model.Item) *replyItem {
	if it == nil {
		return nil
	}
	out := &replyItem{ID: it.ID.String(), TemplateID: it.TemplateID, Owner: it.Owner.String(), Tab: string(it.Tab), Slot: it.Slot, Quantity: it.Quantity, EquipSlot: it.EquipSlot}
	if st := it.Stats; st != nil {
		out.Stats = &replyStats{
			Attack: st.Attack, Defense: st.Defense, STR: st.STR, DEX: st.DEX, INT: st.INT, AGI: st.AGI, VIT: st.VIT,
			Accuracy: st.Accuracy, Evasion: st.Evasion, CritRate: st.CritRate, CritDamage: st.CritDamage, MaxHP: st.MaxHP, MaxMP: st.MaxMP,
		}
	}
	return out
}

func decodeReply(raw []byte) (application.Result, error) {
	if len(raw) == 0 {
		return application.Result{}, nil
	}
	var r reply
	if err := json.Unmarshal(raw, &r); err != nil {
		return application.Result{}, fmt.Errorf("item: stored reply: %w", err)
	}
	res := application.Result{Revision: r.Revision}
	if r.Changed != nil {
		res.Changed = make([]model.Slot, 0, len(r.Changed))
	}
	for _, rs := range r.Changed {
		it, err := decodeItem(rs.Item)
		if err != nil {
			return application.Result{}, err
		}
		res.Changed = append(res.Changed, model.Slot{Tab: gamedata.Tab(rs.Tab), Slot: rs.Slot, Item: it})
	}
	for _, rs := range r.Equipment {
		it, err := decodeItem(rs.Item)
		if err != nil {
			return application.Result{}, err
		}
		res.Equipment = append(res.Equipment, model.WornSlot{Slot: rs.Slot, Item: it})
	}
	for _, ri := range r.Worn {
		it, err := decodeItem(&ri)
		if err != nil {
			return application.Result{}, err
		}
		res.Worn = append(res.Worn, *it)
	}
	return res, nil
}

func decodeItem(ri *replyItem) (*model.Item, error) {
	if ri == nil {
		return nil, nil
	}
	id, err := kernel.ParseItemID(ri.ID)
	if err != nil {
		return nil, fmt.Errorf("item: stored reply: %w", err)
	}
	owner, err := kernel.ParseCharacterID(ri.Owner)
	if err != nil {
		return nil, fmt.Errorf("item: stored reply: %w", err)
	}
	it := &model.Item{ID: id, TemplateID: ri.TemplateID, Owner: owner, Tab: gamedata.Tab(ri.Tab), Slot: ri.Slot, Quantity: ri.Quantity, EquipSlot: ri.EquipSlot}
	if st := ri.Stats; st != nil {
		it.Stats = &progression.ItemStats{
			Attack: st.Attack, Defense: st.Defense, STR: st.STR, DEX: st.DEX, INT: st.INT, AGI: st.AGI, VIT: st.VIT,
			Accuracy: st.Accuracy, Evasion: st.Evasion, CritRate: st.CritRate, CritDamage: st.CritDamage, MaxHP: st.MaxHP, MaxMP: st.MaxMP,
		}
	}
	return it, nil
}
