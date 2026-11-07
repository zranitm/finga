package repository

import (
	"encoding/json"
	"fmt"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// reply is a Result as idempotency_keys stores it; its JSON names are stored, so never rename or remove one, only add.
type reply struct {
	Changed  []replySlot `json:"changed"`
	Revision uint64      `json:"revision,omitempty"`
}

type replySlot struct {
	Tab  string     `json:"tab"`
	Slot int        `json:"slot"`
	Item *replyItem `json:"item,omitempty"`
}

type replyItem struct {
	ID         string `json:"ID"`
	TemplateID string `json:"TemplateID"`
	Owner      string `json:"Owner"`
	Tab        string `json:"Tab"`
	Slot       int    `json:"Slot"`
	Quantity   int    `json:"Quantity"`
}

func encodeReply(res application.Result) ([]byte, error) {
	r := reply{Revision: res.Revision}
	if res.Changed != nil {
		r.Changed = make([]replySlot, 0, len(res.Changed))
	}
	for _, s := range res.Changed {
		rs := replySlot{Tab: string(s.Tab), Slot: s.Slot}
		if it := s.Item; it != nil {
			rs.Item = &replyItem{ID: it.ID.String(), TemplateID: it.TemplateID, Owner: it.Owner.String(), Tab: string(it.Tab), Slot: it.Slot, Quantity: it.Quantity}
		}
		r.Changed = append(r.Changed, rs)
	}
	return json.Marshal(r)
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
		s := model.Slot{Tab: gamedata.Tab(rs.Tab), Slot: rs.Slot}
		if ri := rs.Item; ri != nil {
			id, err := kernel.ParseItemID(ri.ID)
			if err != nil {
				return application.Result{}, fmt.Errorf("item: stored reply: %w", err)
			}
			owner, err := kernel.ParseCharacterID(ri.Owner)
			if err != nil {
				return application.Result{}, fmt.Errorf("item: stored reply: %w", err)
			}
			s.Item = &model.Item{ID: id, TemplateID: ri.TemplateID, Owner: owner, Tab: gamedata.Tab(ri.Tab), Slot: ri.Slot, Quantity: ri.Quantity}
		}
		res.Changed = append(res.Changed, s)
	}
	return res, nil
}
