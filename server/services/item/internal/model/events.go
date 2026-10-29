package model

import (
	"encoding/json"

	"finga/internal/kernel"
)

// itemEvent is the JSON payload of the service's outbox events (page 6:
// ItemLooted, ItemDiscarded; ItemGranted for starter items).
type itemEvent struct {
	ItemID     kernel.ItemID      `json:"item_id"`
	TemplateID string             `json:"template_id"`
	Owner      kernel.CharacterID `json:"owner_id"`
	Quantity   int                `json:"quantity"` // the quantity the command moved
	Tab        string             `json:"tab"`
	Slot       int                `json:"slot"`
	Cause      string             `json:"cause"`
	CauseID    string             `json:"cause_id,omitempty"`
}

// OutboxPayload is the JSON payload of an item outbox event: what changed
// (it), the quantity the command moved, and the cause.
func OutboxPayload(it *Item, quantity int, cause Cause) []byte {
	ev := itemEvent{ItemID: it.ID, TemplateID: it.TemplateID, Owner: it.Owner, Quantity: quantity, Tab: string(it.Tab), Slot: it.Slot, Cause: string(cause.Type)}
	if cause.ID != [16]byte{} {
		ev.CauseID = cause.ID.String()
	}
	b, _ := json.Marshal(ev)
	return b
}
