package item

import (
	"strings"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/item/internal/application"
	"finga/modules/item/internal/model"
)

// Actor is who a command acts for: the account, and the character whose inventory it changes.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// CauseType says what caused an item change.
type CauseType string

// The causes a caller names; a quest's hand-in names its own.
const (
	CauseDrop      CauseType = "drop"
	CausePlayer    CauseType = "player"
	CauseStarter   CauseType = "starter"
	CausePromotion CauseType = "promotion" // a trainer's gift; the cause ID is the gift's key
)

// Cause is why items change, and the drop or request they changed for, if any.
type Cause struct {
	Type CauseType
	ID   uuid.UUID
}

// Item is an item in an inventory slot, or worn in an equip slot.
type Item struct {
	ID         kernel.ItemID
	TemplateID string
	Tab        gamedata.Tab
	Slot       int
	Quantity   int
	Stats      *progression.ItemStats // what it rolled; nil for anything but equipment
	EquipSlot  string                 // where it is worn; empty in the inventory, where Tab and Slot say where it is
}

// Inventory is a character's items as they stood at Revision, the count of their changes.
type Inventory struct {
	Items    []Item
	Revision uint64
}

// Slot is one changed inventory slot; Item is nil when it emptied.
type Slot struct {
	Tab  gamedata.Tab
	Slot int
	Item *Item
}

// EquipSlot is one changed equip slot; Item is nil when it emptied.
type EquipSlot struct {
	Slot string
	Item *Item
}

// Result is what a command changed and the inventory's revision after it, 0 when it changed nothing.
type Result struct {
	Changed   []Slot
	Equipment []EquipSlot // the equip slots an equip, an unequip or a starter grant changed
	Worn      []Item      // after an equip or unequip, every item worn
	Revision  uint64
}

// AddRequest puts a quantity of a template in the inventory, under an idempotency key.
type AddRequest struct {
	Key        string // a UUID, optionally k- prefixed, as every key here
	TemplateID string
	Quantity   int
	Cause      Cause
	Stats      *progression.ItemStats // the drop's roll: required for equipment, refused on anything else
}

// Outcome is how one drop of an AddMany went: refused by the rule in Err, granted, or granted before under its key.
type Outcome struct {
	Err      error
	Replayed bool
	Result   Result // a replayed drop's stored reply
}

// Batch is what an AddMany did: each drop's outcome, in order, and the slots its new grants changed at the revision they took.
type Batch struct {
	Outcomes []Outcome
	Changed  []Slot
	Revision uint64 // 0 when nothing was granted
}

// MoveRequest moves an item to a slot of its tab.
type MoveRequest struct {
	Key    string
	ItemID kernel.ItemID
	Tab    gamedata.Tab
	Slot   int
}

// DiscardRequest destroys a whole item or part of a stack.
type DiscardRequest struct {
	Key      string
	ItemID   kernel.ItemID
	Quantity int
}

// DiscardManyRequest destroys whole items or stacks, all or none.
type DiscardManyRequest struct {
	Key     string
	ItemIDs []kernel.ItemID
}

// UseRequest takes one of a usable item from its stack; Template is what the caller believes it is.
type UseRequest struct {
	Key      string
	ItemID   kernel.ItemID
	Template string
}

// EquipRequest wears an inventory item.
type EquipRequest struct {
	Key       string
	ItemID    kernel.ItemID
	EquipSlot string // rings only: empty, the first empty slot of the item's kind, else the first
	Level     int    // the character's live level, class and own stats, which the item's requirements are checked against
	Class     kernel.Class
	Stats     gamedata.Stats
}

// UnequipRequest puts a worn item back in the Equip tab.
type UnequipRequest struct {
	Key       string
	EquipSlot string
	ToSlot    *int // nil: the first free slot
}

// HandInRequest takes what a quest's collect objectives ask for and gives its item rewards, for the completion-th time it is handed in.
type HandInRequest struct {
	Quest      string
	Completion int
	Take       []gamedata.Grant
	Give       []Give
}

// Give is one reward line; equipment comes one per line, with its roll.
type Give struct {
	TemplateID string
	Quantity   int
	Stats      *progression.ItemStats
}

// Completion names one completion of a quest.
type Completion struct {
	Quest      string
	Completion int
}

// HandedIn is one completion of a quest handed in, with the reply it got.
type HandedIn struct {
	Quest      string
	Completion int
	Result     Result
}

// checkKey accepts a canonical UUID, optionally k- prefixed, kept as sent so a retry across a deploy still finds it.
func checkKey(key string) error {
	id := strings.TrimPrefix(key, "k-")
	if len(id) != 36 {
		return model.ErrBadKey
	}
	if _, err := uuid.Parse(id); err != nil {
		return model.ErrBadKey
	}
	return nil
}

var causeTypes = map[CauseType]model.CauseType{
	CauseDrop:      model.CauseDrop,
	CausePlayer:    model.CausePlayer,
	CauseStarter:   model.CauseStarter,
	CausePromotion: model.CausePromotion,
}

// input checks the request's key and cause, in the order its handler does, so a request malformed both ways names the same fault.
func (r AddRequest) input(keyFirst bool) (application.AddInput, error) {
	t, ok := causeTypes[r.Cause.Type]
	err := checkKey(r.Key)
	switch {
	case err != nil && (keyFirst || ok):
		return application.AddInput{}, err
	case !ok:
		return application.AddInput{}, model.ErrBadCause
	}
	return application.AddInput{Key: r.Key, TemplateID: r.TemplateID, Quantity: r.Quantity, Cause: model.Cause{Type: t, ID: r.Cause.ID}, Stats: r.Stats}, nil
}

func (r HandInRequest) input() application.HandInInput {
	in := application.HandInInput{Quest: r.Quest, Completion: r.Completion, Take: r.Take}
	for _, g := range r.Give {
		in.Give = append(in.Give, application.Give(g))
	}
	return in
}

func itemOf(it model.Item) Item {
	return Item{ID: it.ID, TemplateID: it.TemplateID, Tab: it.Tab, Slot: it.Slot, Quantity: it.Quantity, Stats: it.Stats, EquipSlot: it.EquipSlot}
}

func itemsOf(its []model.Item) []Item {
	out := make([]Item, 0, len(its))
	for _, it := range its {
		out = append(out, itemOf(it))
	}
	return out
}

func itemAt(it *model.Item) *Item {
	if it == nil {
		return nil
	}
	out := itemOf(*it)
	return &out
}

func slotsOf(slots []model.Slot) []Slot {
	out := make([]Slot, 0, len(slots))
	for _, s := range slots {
		out = append(out, Slot{Tab: s.Tab, Slot: s.Slot, Item: itemAt(s.Item)})
	}
	return out
}

func resultOf(res application.Result) Result {
	out := Result{Changed: slotsOf(res.Changed), Worn: itemsOf(res.Worn), Revision: res.Revision}
	for _, s := range res.Equipment {
		out.Equipment = append(out.Equipment, EquipSlot{Slot: s.Slot, Item: itemAt(s.Item)})
	}
	return out
}
