package repository

import (
	"context"
	"errors"
	"testing"

	"finga/internal/kernel"
	"finga/modules/item/internal/application"
	"finga/modules/item/internal/model"
)

func TestUse(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "a1", "red_potion", 2)
	h.add(t, "a2", "slime_gel", 3)
	var potion, gel model.Item
	for _, it := range h.inventory(t) {
		switch it.TemplateID {
		case "red_potion":
			potion = it
		case "slime_gel":
			gel = it
		}
	}
	t.Run("one from the stack, with its history and its notice", func(t *testing.T) {
		res, replayed, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u1", ItemID: potion.ID, Template: "red_potion"})
		if err != nil || replayed || len(res.Changed) != 1 || res.Changed[0].Item.Quantity != 1 || res.Revision == 0 {
			t.Fatalf("result %+v replayed %v err %v", res, replayed, err)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'used' AND quantity_delta = -1 AND item_id = $1`, potion.ID) != 1 {
			t.Fatal("no used event")
		}
		if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemUsed'`) != 1 {
			t.Fatal("no ItemUsed outbox event")
		}
	})
	t.Run("the same key again changes nothing more, and says so", func(t *testing.T) {
		res, replayed, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u1", ItemID: potion.ID, Template: "red_potion"})
		if err != nil || !replayed || res.Changed[0].Item.Quantity != 1 {
			t.Fatalf("replay %+v replayed %v err %v", res, replayed, err)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'used'`) != 1 {
			t.Fatal("the replay used another")
		}
	})
	t.Run("refused: a template that can't be used, the wrong template, and someone else's item", func(t *testing.T) {
		if _, _, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u2", ItemID: gel.ID, Template: "slime_gel"}); !errors.Is(err, model.ErrNotUsable) {
			t.Fatalf("a material: %v", err)
		}
		if _, _, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u3", ItemID: gel.ID, Template: "red_potion"}); !errors.Is(err, model.ErrNotUsable) {
			t.Fatalf("a material named as a potion: %v", err)
		}
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, _, err := h.svc.Use(ctx, other, application.UseInput{Key: "u4", ItemID: potion.ID, Template: "red_potion"}); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("someone else's: %v", err)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'used'`) != 1 {
			t.Fatal("a refused use took something")
		}
	})
	t.Run("the last one empties its slot", func(t *testing.T) {
		res, _, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u5", ItemID: potion.ID, Template: "red_potion"})
		if err != nil || len(res.Changed) != 1 || res.Changed[0].Item != nil || res.Changed[0].Slot != potion.Slot {
			t.Fatalf("result %+v err %v", res, err)
		}
		if _, _, err := h.svc.Use(ctx, h.actor, application.UseInput{Key: "u6", ItemID: potion.ID, Template: "red_potion"}); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("a used-up potion: %v", err)
		}
	})
}
