package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"finga/internal/gamedata"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

func (h *harness) drop(key, template string, qty int) application.AddInput {
	return application.AddInput{Key: key, TemplateID: template, Quantity: qty, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: h.roll(template)}
}

func (h *harness) addMany(t *testing.T, drops ...application.AddInput) application.Batch {
	t.Helper()
	b, err := h.svc.AddMany(context.Background(), h.actor, drops)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (h *harness) stored(t *testing.T, key string) bool {
	t.Helper()
	return h.count(t, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, key) == 1
}

// quantity is how much of template the inventory holds, in all its stacks.
func (h *harness) quantity(t *testing.T, template string) int {
	t.Helper()
	n := 0
	for _, it := range h.inventory(t) {
		if it.TemplateID == template {
			n += it.Quantity
		}
	}
	return n
}

func granted(t *testing.T, b application.Batch, which ...int) {
	t.Helper()
	for _, i := range which {
		if out := b.Outcomes[i]; out.Err != nil || out.Replayed {
			t.Fatalf("drop %d: %+v, want granted now", i, out)
		}
	}
}

func TestAddMany(t *testing.T) {
	t.Run("new drops are granted together, at one revision", func(t *testing.T) {
		h := newHarness(t)
		before := h.revision(t)
		b := h.addMany(t, h.drop("a1", "slime_gel", 2), h.drop("a2", "slime_gel", 3), h.drop("a3", "iron_sword", 1))
		granted(t, b, 0, 1, 2)
		if b.Revision != before+1 || h.revision(t) != before+1 {
			t.Fatalf("revision %d, read %d; want %d", b.Revision, h.revision(t), before+1)
		}
		// The gel's slot appears once, as the second drop left it.
		if len(b.Changed) != 2 || b.Changed[0].Tab != gamedata.Etc || b.Changed[0].Item.Quantity != 5 || b.Changed[1].Tab != gamedata.Equip {
			t.Fatalf("changed %+v", b.Changed)
		}
		for _, k := range []string{"a1", "a2", "a3"} {
			if !h.stored(t, k) {
				t.Fatalf("no reply stored under %s", k)
			}
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("a drop granted before answers its stored reply, and the new ones are granted", func(t *testing.T) {
		h := newHarness(t)
		gel := h.drop("b1", "slime_gel", 2)
		first, err := h.svc.Add(context.Background(), h.actor, gel)
		if err != nil {
			t.Fatal(err)
		}
		b := h.addMany(t, gel, h.drop("b2", "red_potion", 1))
		if out := b.Outcomes[0]; !out.Replayed || !reflect.DeepEqual(out.Result, first) {
			t.Fatalf("replayed %+v, want Add's reply %+v", out, first)
		}
		granted(t, b, 1)
		if len(b.Changed) != 1 || b.Changed[0].Tab != gamedata.Use || b.Revision != first.Revision+1 {
			t.Fatalf("changed %+v at %d", b.Changed, b.Revision)
		}
		if n := h.quantity(t, "slime_gel"); n != 2 {
			t.Fatalf("%d gel; the drop was added twice", n)
		}
	})

	t.Run("one key twice in a batch grants once, and the second answers the first's reply", func(t *testing.T) {
		h := newHarness(t)
		gel := h.drop("c1", "slime_gel", 2)
		b := h.addMany(t, gel, gel)
		granted(t, b, 0)
		if out := b.Outcomes[1]; !out.Replayed || out.Result.Revision != b.Revision {
			t.Fatalf("the repeat %+v", out)
		}
		if n := h.quantity(t, "slime_gel"); n != 2 {
			t.Fatalf("%d gel, want 2", n)
		}
	})

	t.Run("a drop that half fills a stack and then doesn't fit rolls back alone, and stores no reply", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "d0", "slime_gel", 99)
		for i := range model.SlotsPerTab - 1 {
			h.add(t, fmt.Sprintf("fill-%d", i), "mushroom_cap", 100)
		}
		before := h.revision(t)
		b := h.addMany(t, h.drop("d1", "slime_gel", 3), h.drop("d2", "red_potion", 1))
		if !errors.Is(b.Outcomes[0].Err, model.ErrEtcTabFull) {
			t.Fatalf("the gel %+v, want inventory full", b.Outcomes[0])
		}
		granted(t, b, 1)
		if n := h.quantity(t, "slime_gel"); n != 99 {
			t.Fatalf("%d gel; the stack's one free place stayed filled", n)
		}
		if h.stored(t, "d1") || !h.stored(t, "d2") {
			t.Fatal("a refused drop stored a reply, or a granted one none")
		}
		if b.Revision != before+1 {
			t.Fatalf("revision %d, want %d", b.Revision, before+1)
		}
		checkInvariants(t, h.pool, h.data)
		// Its key isn't spent: once there is room, the same drop is granted.
		var caps model.Item
		for _, it := range h.inventory(t) {
			if it.TemplateID == "mushroom_cap" {
				caps = it
			}
		}
		if _, err := h.svc.Discard(context.Background(), h.actor, application.DiscardInput{Key: "d3", ItemID: caps.ID, Quantity: 100}); err != nil {
			t.Fatal(err)
		}
		granted(t, h.addMany(t, h.drop("d1", "slime_gel", 3)), 0)
	})

	t.Run("a key reused with other arguments refuses that drop only", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "e1", "slime_gel", 1)
		b := h.addMany(t, h.drop("e1", "slime_gel", 2), h.drop("e2", "slime_gel", 1))
		if !errors.Is(b.Outcomes[0].Err, model.ErrKeyReused) {
			t.Fatalf("the reused key %+v", b.Outcomes[0])
		}
		granted(t, b, 1)
	})

	t.Run("a batch the rules refuse whole takes no revision and writes nothing", func(t *testing.T) {
		h := newHarness(t)
		before, rows := h.revision(t), h.count(t, `SELECT count(*) FROM items`)
		unrolled := h.drop("f2", "iron_sword", 1)
		unrolled.Stats = nil
		b := h.addMany(t, h.drop("f1", "dragon_scale", 1), unrolled, h.drop("f3", "slime_gel", 0))
		for i, rule := range []error{model.ErrUnknownItem, model.ErrBadRoll, model.ErrBadQuantity} {
			if !errors.Is(b.Outcomes[i].Err, rule) {
				t.Fatalf("drop %d: %+v, want %v", i, b.Outcomes[i], rule)
			}
		}
		if b.Revision != 0 || len(b.Changed) != 0 || h.revision(t) != before || h.count(t, `SELECT count(*) FROM items`) != rows {
			t.Fatal("a refused batch changed something")
		}
	})

	t.Run("equipment found without a roll keeps the one it is given, whatever becomes of the drops", func(t *testing.T) {
		h := newHarness(t)
		sword := h.add(t, "h0", "iron_sword", 1).Changed[0].Item
		if _, err := h.pool.Exec(context.Background(), `DELETE FROM item_rolls WHERE item_id = $1`, sword.ID); err != nil {
			t.Fatal(err)
		}
		b := h.addMany(t, h.drop("h1", "slime_gel", 0))
		if !errors.Is(b.Outcomes[0].Err, model.ErrBadQuantity) {
			t.Fatalf("the drop %+v", b.Outcomes[0])
		}
		if h.count(t, `SELECT count(*) FROM item_rolls WHERE item_id = $1 AND physical_attack = 16`, sword.ID) != 1 {
			t.Fatal("the sword's lowest roll wasn't kept")
		}
		checkInvariants(t, h.pool, h.data)
	})

	t.Run("a batch of no drops, or of more than the limit, is refused", func(t *testing.T) {
		h := newHarness(t)
		many := make([]application.AddInput, application.MaxBatch+1)
		for i := range many {
			many[i] = h.drop(fmt.Sprintf("g%d", i), "slime_gel", 1)
		}
		for _, drops := range [][]application.AddInput{nil, many} {
			if _, err := h.svc.AddMany(context.Background(), h.actor, drops); !errors.Is(err, model.ErrBadBatch) {
				t.Fatalf("%d drops: %v", len(drops), err)
			}
		}
	})
}

// A batch and an Add of the same key, at once, give one item: whichever runs first, the other replays it.
func TestABatchAndAnAddOfOneKeyGiveOneItem(t *testing.T) {
	for range 10 {
		h := newHarness(t)
		gel := h.drop(uuidv7.NewString(), "slime_gel", 2)
		var wg sync.WaitGroup
		var batchErr, addErr error
		wg.Go(func() {
			_, batchErr = h.svc.AddMany(context.Background(), h.actor, []application.AddInput{h.drop(uuidv7.NewString(), "red_potion", 1), gel})
		})
		wg.Go(func() { _, addErr = h.svc.Add(context.Background(), h.actor, gel) })
		wg.Wait()
		if batchErr != nil || addErr != nil {
			t.Fatalf("batch %v, add %v", batchErr, addErr)
		}
		if n := h.count(t, `SELECT coalesce(sum(quantity), 0) FROM items WHERE template_id = 'slime_gel'`); n != 2 {
			t.Fatalf("%d gel, want the one drop's 2", n)
		}
		checkInvariants(t, h.pool, h.data)
	}
}
