package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// handIn hands quest in under key, taking take and giving give, each equipment reward with its top roll.
func (h *harness) handIn(key, quest string, take []model.Grant, give ...application.Give) (application.Result, error) {
	for k := range give {
		if give[k].Stats == nil {
			give[k].Stats = h.roll(give[k].TemplateID)
		}
	}
	return h.svc.HandIn(context.Background(), h.actor, application.HandInInput{Key: key, Quest: quest, Take: take, Give: give})
}

func gel(n int) []model.Grant { return []model.Grant{{Item: "slime_gel", Quantity: n}} }

func TestHandIn(t *testing.T) {
	ctx := context.Background()

	t.Run("takes from the smallest stacks first and gives the rewards, at one revision", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 98)
		h.add(t, "g2", "slime_gel", 10) // fills slot 0 to 100, opens slot 1 with 8
		before := h.revision(t)
		key := uuidv7.NewString()
		res, err := h.handIn(key, "sticky_samples", gel(5), application.Give{TemplateID: "red_potion", Quantity: 10}, application.Give{TemplateID: "leather_cap", Quantity: 1})
		if err != nil {
			t.Fatal(err)
		}
		if res.Revision != before+1 || h.revision(t) != before+1 {
			t.Fatalf("revision %d after %d", res.Revision, before)
		}
		want := []struct {
			tab      gamedata.Tab
			slot     int
			template string
			quantity int
		}{{gamedata.Etc, 1, "slime_gel", 3}, {gamedata.Use, 0, "red_potion", 10}, {gamedata.Equip, 0, "leather_cap", 1}}
		if len(res.Changed) != len(want) {
			t.Fatalf("changed %+v", res.Changed)
		}
		for k, w := range want {
			got := res.Changed[k]
			if got.Tab != w.tab || got.Slot != w.slot || got.Item == nil || got.Item.TemplateID != w.template || got.Item.Quantity != w.quantity {
				t.Fatalf("changed %d: %+v, want %+v", k, got, w)
			}
		}
		if cap := res.Changed[2].Item; cap.Stats == nil || *cap.Stats != *h.roll("leather_cap") {
			t.Fatalf("the cap's roll %+v", cap.Stats)
		}
		if h.quantity(t, "slime_gel") != 103 {
			t.Fatalf("gel left %d", h.quantity(t, "slime_gel"))
		}
		// The next hand-in empties the small stack before it touches the full one.
		res, err = h.handIn(uuidv7.NewString(), "sticky_samples", gel(5))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) != 2 || res.Changed[0].Slot != 1 || res.Changed[0].Item != nil || res.Changed[1].Slot != 0 || res.Changed[1].Item.Quantity != 98 {
			t.Fatalf("changed %+v", res.Changed)
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("each item's history names the quest, under the hand-in's key", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 5)
		key := uuidv7.NewString()
		if _, err := h.handIn(key, "sticky_samples", gel(5), application.Give{TemplateID: "red_potion", Quantity: 3}); err != nil {
			t.Fatal(err)
		}
		id := uuid.MustParse(key)
		if n := h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'handed_in' AND quantity_delta = -5 AND cause_type = 'quest' AND quest_id = 'sticky_samples' AND cause_id = $1`, id); n != 1 {
			t.Fatalf("%d handed_in events", n)
		}
		if n := h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'created' AND quantity_delta = 3 AND cause_type = 'quest' AND quest_id = 'sticky_samples' AND cause_id = $1`, id); n != 1 {
			t.Fatalf("%d reward events", n)
		}
		if n := h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemHandedIn' AND convert_from(payload, 'UTF8')::jsonb @> '{"quest_id": "sticky_samples", "quantity": -5, "cause": "quest"}'`); n != 1 {
			t.Fatalf("%d hand-in notices", n)
		}
	})

	t.Run("asking for more than the stacks hold changes nothing", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 4)
		before, inv := h.revision(t), h.inventory(t)
		_, err := h.handIn(uuidv7.NewString(), "sticky_samples", gel(5), application.Give{TemplateID: "red_potion", Quantity: 1})
		if !errors.Is(err, model.ErrNotEnoughItems) {
			t.Fatalf("err = %v", err)
		}
		if h.revision(t) != before || !reflect.DeepEqual(h.inventory(t), inv) || h.count(t, `SELECT count(*) FROM item_events WHERE cause_type = 'quest'`) != 0 {
			t.Fatalf("something changed: %+v", h.inventory(t))
		}
	})

	t.Run("worn items are never taken", func(t *testing.T) {
		h := newHarness(t)
		worn := h.added(t, "leather_cap")
		if _, err := h.equip(uuidv7.NewString(), worn.ID, "", 1); err != nil {
			t.Fatal(err)
		}
		take := []model.Grant{{Item: "leather_cap", Quantity: 1}}
		if _, err := h.handIn(uuidv7.NewString(), "q", take); !errors.Is(err, model.ErrNotEnoughItems) {
			t.Fatalf("err = %v", err)
		}
		spare := h.added(t, "leather_cap")
		if _, err := h.handIn(uuidv7.NewString(), "q", take); err != nil {
			t.Fatal(err)
		}
		if h.held(t, worn.ID).EquipSlot != "hat" || h.count(t, `SELECT count(*) FROM items WHERE id = $1 AND location = 'destroyed'`, spare.ID) != 1 {
			t.Fatalf("inventory %+v", h.inventory(t))
		}
	})

	t.Run("a reward with no room takes nothing", func(t *testing.T) {
		h := newHarness(t)
		for k := range model.SlotsPerTab {
			h.add(t, uuidv7.NewString(), []string{"red_potion", "blue_potion"}[k%2], 100)
		}
		h.add(t, "g1", "slime_gel", 5)
		before, inv := h.revision(t), h.inventory(t)
		_, err := h.handIn(uuidv7.NewString(), "sticky_samples", gel(5), application.Give{TemplateID: "red_potion", Quantity: 1})
		if !errors.Is(err, model.ErrInventoryFull) {
			t.Fatalf("err = %v", err)
		}
		if h.revision(t) != before || !reflect.DeepEqual(h.inventory(t), inv) {
			t.Fatal("something changed")
		}
		// A full stack handed in frees its slot for the reward.
		res, err := h.handIn(uuidv7.NewString(), "q", []model.Grant{{Item: "blue_potion", Quantity: 100}}, application.Give{TemplateID: "red_potion", Quantity: 60})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) != 1 || res.Changed[0].Tab != gamedata.Use || res.Changed[0].Item == nil || res.Changed[0].Item.TemplateID != "red_potion" || res.Changed[0].Item.Quantity != 60 {
			t.Fatalf("changed %+v", res.Changed)
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("the same key again replays the stored reply, whatever the reward rolls this time", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 10)
		key := uuidv7.NewString()
		first, err := h.handIn(key, "sticky_samples", gel(5), application.Give{TemplateID: "leather_cap", Quantity: 1})
		if err != nil {
			t.Fatal(err)
		}
		tmpl, _ := h.data.Item("leather_cap")
		low := progression.Lowest(tmpl.Rolls)
		again, err := h.handIn(key, "sticky_samples", gel(5), application.Give{TemplateID: "leather_cap", Quantity: 1, Stats: &low})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again, first) || h.quantity(t, "slime_gel") != 5 || h.quantity(t, "leather_cap") != 1 {
			t.Fatalf("replay %+v, first %+v", again, first)
		}
		if _, err := h.handIn(key, "sticky_samples", gel(4)); !errors.Is(err, model.ErrKeyReused) {
			t.Fatalf("another take under the key: %v", err)
		}
	})

	t.Run("a hand-in with nothing to take or give changes nothing", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.handIn(uuidv7.NewString(), "slime_cleanup", nil)
		if err != nil || res.Revision != 0 || len(res.Changed) != 0 {
			t.Fatalf("res %+v, %v", res, err)
		}
	})

	t.Run("refused before it starts", func(t *testing.T) {
		h := newHarness(t)
		lines := make([]model.Grant, application.MaxHandInLines+1)
		for k := range lines {
			lines[k] = model.Grant{Item: "slime_gel", Quantity: 1}
		}
		top := h.roll("leather_cap")
		tests := []struct {
			name string
			in   application.HandInInput
			want error
		}{
			{"no quest", application.HandInInput{Take: gel(1)}, model.ErrBadCause},
			{"too many lines", application.HandInInput{Quest: "q", Take: lines}, model.ErrBadBatch},
			{"an unknown item to take", application.HandInInput{Quest: "q", Take: []model.Grant{{Item: "dragon_scale", Quantity: 1}}}, model.ErrUnknownItem},
			{"nothing of an item to take", application.HandInInput{Quest: "q", Take: gel(0)}, model.ErrBadQuantity},
			{"an unknown reward", application.HandInInput{Quest: "q", Give: []application.Give{{TemplateID: "elixir", Quantity: 1}}}, model.ErrUnknownItem},
			{"two pieces of equipment on one line", application.HandInInput{Quest: "q", Give: []application.Give{{TemplateID: "leather_cap", Quantity: 2, Stats: top}}}, model.ErrBadQuantity},
			{"equipment without a roll", application.HandInInput{Quest: "q", Give: []application.Give{{TemplateID: "leather_cap", Quantity: 1}}}, model.ErrBadRoll},
			{"a potion with a roll", application.HandInInput{Quest: "q", Give: []application.Give{{TemplateID: "red_potion", Quantity: 1, Stats: top}}}, model.ErrBadRoll},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.in.Key = uuidv7.NewString()
				if _, err := h.svc.HandIn(ctx, h.actor, tt.in); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		}
		if h.count(t, `SELECT count(*) FROM idempotency_keys`) != 0 {
			t.Fatal("a refused hand-in stored a reply")
		}
	})
}
