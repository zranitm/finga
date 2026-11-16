package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/modules/item/internal/model"
	"finga/pkg/outbox"
)

const topic = "item"

var eventTypes = map[model.NoticeKind]string{
	model.NoticeGranted:   "ItemGranted",
	model.NoticeLooted:    "ItemLooted",
	model.NoticeDiscarded: "ItemDiscarded",
	model.NoticeHandedIn:  "ItemHandedIn",
	model.NoticeUsed:      "ItemUsed",
}

// itemEvent is the payload other services read.
type itemEvent struct {
	ItemID     kernel.ItemID      `json:"item_id"`
	TemplateID string             `json:"template_id"`
	Owner      kernel.CharacterID `json:"owner_id"`
	Quantity   int                `json:"quantity"`
	Tab        string             `json:"tab"`
	Slot       int                `json:"slot"`
	Cause      string             `json:"cause"`
	CauseID    string             `json:"cause_id,omitempty"`
	QuestID    string             `json:"quest_id,omitempty"`
}

// notices writes each notice to the outbox as an item topic event.
type notices struct{ w outbox.Writer }

// Send adds the notice to the transaction's outbox.
func (n notices) Send(ctx context.Context, x model.Notice) error {
	typ, ok := eventTypes[x.Kind]
	if !ok {
		return fmt.Errorf("item: notice kind %q has no event type", x.Kind)
	}
	ev := itemEvent{ItemID: x.Item.ID, TemplateID: x.Item.TemplateID, Owner: x.Item.Owner, Quantity: x.Quantity, Tab: string(x.Item.Tab), Slot: x.Item.Slot, Cause: string(x.Cause.Type), QuestID: x.Cause.Quest}
	if x.Cause.ID != uuid.Nil {
		ev.CauseID = x.Cause.ID.String()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return n.w.Add(ctx, outbox.Event{Topic: topic, Key: x.Item.ID.String(), Type: typ, Payload: payload, OccurredAt: x.OccurredAt})
}
