package repository

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/item/internal/application"
	"finga/modules/item/internal/model"
	"finga/pkg/uuidv7"
)

// handIn hands quest in for the completion-th time, taking take and giving give, each equipment reward with its top roll.
func (h *harness) handIn(quest string, completion int, take []model.Grant, give ...application.Give) (application.Result, error) {
	for k := range give {
		if give[k].Stats == nil {
			give[k].Stats = h.roll(give[k].TemplateID)
		}
	}
	return h.svc.HandIn(context.Background(), h.actor, application.HandInInput{Quest: quest, Completion: completion, Take: take, Give: give})
}

// handInID is the ID a completion was recorded under.
func (h *harness) handInID(t *testing.T, quest string, completion int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.pool.QueryRow(context.Background(), `SELECT id FROM quest_hand_ins WHERE character_id = $1 AND quest_id = $2 AND completion = $3`, uuid.UUID(h.actor.Character), quest, completion).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func gel(n int) []model.Grant { return []model.Grant{{Item: "slime_gel", Quantity: n}} }

func TestHandIn(t *testing.T) {
	ctx := context.Background()

	t.Run("takes from the smallest stacks first and gives the rewards, at one revision", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 98)
		h.add(t, "g2", "slime_gel", 10) // fills slot 0 to 100, opens slot 1 with 8
		before := h.revision(t)
		res, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 10}, application.Give{TemplateID: "leather_cap", Quantity: 1})
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
		// The next completion empties the small stack before it touches the full one.
		res, err = h.handIn("sticky_samples", 2, gel(5))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) != 2 || res.Changed[0].Slot != 1 || res.Changed[0].Item != nil || res.Changed[1].Slot != 0 || res.Changed[1].Item.Quantity != 98 {
			t.Fatalf("changed %+v", res.Changed)
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("each item's history names the quest, under the hand-in's ID", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 5)
		if _, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 3}); err != nil {
			t.Fatal(err)
		}
		id := h.handInID(t, "sticky_samples", 1)
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

	t.Run("asking for more than the stacks hold changes nothing and records nothing", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 4)
		before, inv := h.revision(t), h.inventory(t)
		_, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 1})
		if !errors.Is(err, model.ErrNotEnoughItems) {
			t.Fatalf("err = %v", err)
		}
		if h.revision(t) != before || !reflect.DeepEqual(h.inventory(t), inv) || h.count(t, `SELECT count(*) FROM item_events WHERE cause_type = 'quest'`) != 0 {
			t.Fatalf("something changed: %+v", h.inventory(t))
		}
		if h.count(t, `SELECT count(*) FROM quest_hand_ins`) != 0 {
			t.Fatal("a refused hand-in was recorded")
		}
		// With the items, the same completion goes through.
		h.add(t, "g2", "slime_gel", 1)
		if _, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 1}); err != nil || h.quantity(t, "slime_gel") != 0 {
			t.Fatalf("err = %v, gel left %d", err, h.quantity(t, "slime_gel"))
		}
	})

	t.Run("worn items are never taken", func(t *testing.T) {
		h := newHarness(t)
		worn := h.added(t, "leather_cap")
		if _, err := h.equip(uuidv7.NewString(), worn.ID, "", 1); err != nil {
			t.Fatal(err)
		}
		take := []model.Grant{{Item: "leather_cap", Quantity: 1}}
		if _, err := h.handIn("q", 1, take); !errors.Is(err, model.ErrNotEnoughItems) {
			t.Fatalf("err = %v", err)
		}
		spare := h.added(t, "leather_cap")
		if _, err := h.handIn("q", 1, take); err != nil {
			t.Fatal(err)
		}
		if h.held(t, worn.ID).EquipSlot != "hat" || h.count(t, `SELECT count(*) FROM items WHERE id = $1 AND location = 'destroyed'`, spare.ID) != 1 {
			t.Fatalf("inventory %+v", h.inventory(t))
		}
	})

	t.Run("a reward with no room takes nothing", func(t *testing.T) {
		h := newHarness(t)
		for k := range model.SlotsPerTab {
			h.add(t, uuidv7.NewString(), []string{"red_potion", "white_potion"}[k%2], 100)
		}
		h.add(t, "g1", "slime_gel", 5)
		before, inv := h.revision(t), h.inventory(t)
		_, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 1})
		if !errors.Is(err, model.ErrUseTabFull) {
			t.Fatalf("err = %v", err)
		}
		if h.revision(t) != before || !reflect.DeepEqual(h.inventory(t), inv) {
			t.Fatal("something changed")
		}
		// A full stack handed in frees its slot for the reward.
		res, err := h.handIn("q", 1, []model.Grant{{Item: "white_potion", Quantity: 100}}, application.Give{TemplateID: "red_potion", Quantity: 60})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) != 1 || res.Changed[0].Tab != gamedata.Use || res.Changed[0].Item == nil || res.Changed[0].Item.TemplateID != "red_potion" || res.Changed[0].Item.Quantity != 60 {
			t.Fatalf("changed %+v", res.Changed)
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("the same completion again replays the stored reply, whatever its rolls and lines, with no idempotency key to prune", func(t *testing.T) {
		var logs bytes.Buffer
		h := newHarness(t)
		h.svc = application.NewService(application.Deps{Store: h.store, Content: h.data, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
		h.add(t, "g1", "slime_gel", 10)
		first, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "leather_cap", Quantity: 1})
		if err != nil {
			t.Fatal(err)
		}
		if h.count(t, `SELECT count(*) FROM idempotency_keys WHERE scope LIKE 'item.hand_in%'`) != 0 {
			t.Fatal("a hand-in stored an idempotency key")
		}
		tmpl, _ := h.data.Item("leather_cap")
		low := progression.Lowest(tmpl.Rolls)
		again, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "leather_cap", Quantity: 1, Stats: &low})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(again, first) || h.quantity(t, "slime_gel") != 5 || h.quantity(t, "leather_cap") != 1 || logs.Len() != 0 {
			t.Fatalf("replay %+v, first %+v, logs %s", again, first, logs.String())
		}
		other, err := h.handIn("sticky_samples", 1, gel(4))
		if err != nil || !reflect.DeepEqual(other, first) || h.quantity(t, "slime_gel") != 5 {
			t.Fatalf("other lines: %+v, %v", other, err)
		}
		if !strings.Contains(logs.String(), "other lines") {
			t.Fatalf("logs %s", logs.String())
		}
	})

	t.Run("one completion handed in at once by several goes through once", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "g1", "slime_gel", 10)
		const n = 6
		results, errs := make([]application.Result, n), make([]error, n)
		var wg sync.WaitGroup
		for k := range n {
			wg.Go(func() {
				results[k], errs[k] = h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 10})
			})
		}
		wg.Wait()
		for k := range n {
			if errs[k] != nil || !reflect.DeepEqual(results[k], results[0]) {
				t.Fatalf("hand-in %d: %+v, %v; the first %+v", k, results[k], errs[k], results[0])
			}
		}
		if h.quantity(t, "slime_gel") != 5 || h.quantity(t, "red_potion") != 10 || h.count(t, `SELECT count(*) FROM quest_hand_ins`) != 1 {
			t.Fatalf("inventory %+v", h.inventory(t))
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("a hand-in with nothing to take or give changes nothing", func(t *testing.T) {
		h := newHarness(t)
		res, err := h.handIn("slime_cleanup", 1, nil)
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
			{"no quest", application.HandInInput{Completion: 1, Take: gel(1)}, model.ErrBadCause},
			{"no completion", application.HandInInput{Quest: "q", Take: gel(1)}, model.ErrBadCause},
			{"too many lines", application.HandInInput{Quest: "q", Completion: 1, Take: lines}, model.ErrBadBatch},
			{"an unknown item to take", application.HandInInput{Quest: "q", Completion: 1, Take: []model.Grant{{Item: "dragon_scale", Quantity: 1}}}, model.ErrUnknownItem},
			{"nothing of an item to take", application.HandInInput{Quest: "q", Completion: 1, Take: gel(0)}, model.ErrBadQuantity},
			{"an unknown reward", application.HandInInput{Quest: "q", Completion: 1, Give: []application.Give{{TemplateID: "elixir", Quantity: 1}}}, model.ErrUnknownItem},
			{"two pieces of equipment on one line", application.HandInInput{Quest: "q", Completion: 1, Give: []application.Give{{TemplateID: "leather_cap", Quantity: 2, Stats: top}}}, model.ErrBadQuantity},
			{"equipment without a roll", application.HandInInput{Quest: "q", Completion: 1, Give: []application.Give{{TemplateID: "leather_cap", Quantity: 1}}}, model.ErrBadRoll},
			{"a potion with a roll", application.HandInInput{Quest: "q", Completion: 1, Give: []application.Give{{TemplateID: "red_potion", Quantity: 1, Stats: top}}}, model.ErrBadRoll},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := h.svc.HandIn(ctx, h.actor, tt.in); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		}
		if h.count(t, `SELECT count(*) FROM quest_hand_ins`) != 0 {
			t.Fatal("a refused hand-in was recorded")
		}
	})
}

func TestHandedIn(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "g1", "slime_gel", 10)
	first, err := h.handIn("sticky_samples", 1, gel(5), application.Give{TemplateID: "red_potion", Quantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.handIn("sticky_samples", 2, gel(5))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("lists only the completions handed in, each with its reply", func(t *testing.T) {
		got, err := h.svc.HandedIn(ctx, h.actor.Character, []application.Completion{{Quest: "sticky_samples", Completion: 2}, {Quest: "sticky_samples", Completion: 3}, {Quest: "slime_cleanup", Completion: 1}, {Quest: "sticky_samples", Completion: 1}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Completion != 1 || got[1].Completion != 2 || !reflect.DeepEqual(got[0].Result, first) || !reflect.DeepEqual(got[1].Result, second) {
			t.Fatalf("handed in %+v", got)
		}
		if got[0].ID != h.handInID(t, "sticky_samples", 1) {
			t.Fatalf("ID %s", got[0].ID)
		}
	})

	t.Run("another character's are not its", func(t *testing.T) {
		got, err := h.svc.HandedIn(ctx, kernel.NewCharacterID(), []application.Completion{{Quest: "sticky_samples", Completion: 1}})
		if err != nil || len(got) != 0 {
			t.Fatalf("handed in %+v, %v", got, err)
		}
	})

	t.Run("asking about none finds none; asking about too many is refused", func(t *testing.T) {
		if got, err := h.svc.HandedIn(ctx, h.actor.Character, nil); err != nil || got != nil {
			t.Fatalf("handed in %+v, %v", got, err)
		}
		if _, err := h.svc.HandedIn(ctx, h.actor.Character, make([]application.Completion, application.MaxHandedIn+1)); !errors.Is(err, model.ErrBadBatch) {
			t.Fatalf("err = %v", err)
		}
	})
}
