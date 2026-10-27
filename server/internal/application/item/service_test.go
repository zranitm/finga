package item

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/application/character"
	"finga/internal/gamedata"
	"finga/internal/model"
)

// These rules are about transactions, so they run against real Postgres.

type harness struct {
	pool  *pgxpool.Pool
	store *Postgres
	svc   *Service
	data  *gamedata.Data
	actor Actor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	data, err := gamedata.Load("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	pool := cluster.DB(t)
	store := NewPostgres(pool)
	return &harness{pool: pool, store: store, svc: NewService(store, data, Options{}), data: data, actor: Actor{Account: model.NewAccountID(), Character: model.NewCharacterID()}}
}

func (h *harness) add(t *testing.T, key, template string, qty int) Result {
	t.Helper()
	res, err := h.svc.Add(context.Background(), h.actor, AddInput{Key: key, TemplateID: template, Quantity: qty, Cause: Cause{Type: CauseDrop, ID: uuid.New()}})
	if err != nil {
		t.Fatalf("add %s x%d: %v", template, qty, err)
	}
	return res
}

func (h *harness) inventory(t *testing.T) []Item {
	t.Helper()
	inv, err := h.svc.Inventory(context.Background(), h.actor.Character)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdd(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	t.Run("a stackable item goes to the first free slot of its tab", func(t *testing.T) {
		res := h.add(t, "k1", "red_potion", 30)
		if len(res.Changed) != 1 || res.Changed[0].Tab != gamedata.Use || res.Changed[0].Slot != 0 || res.Changed[0].Item.Quantity != 30 {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("more of it goes into the existing stack", func(t *testing.T) {
		res := h.add(t, "k2", "red_potion", 50)
		inv := h.inventory(t)
		if len(inv) != 1 || inv[0].Quantity != 80 || len(res.Changed) != 1 || res.Changed[0].Item.Quantity != 80 {
			t.Fatalf("inventory %+v result %+v", inv, res)
		}
	})
	t.Run("overflow fills the stack and opens a new slot", func(t *testing.T) {
		res := h.add(t, "k3", "red_potion", 50) // 20 fit, 30 overflow
		inv := h.inventory(t)
		if len(inv) != 2 || inv[0].Quantity != 100 || inv[1].Quantity != 30 || inv[1].Slot != 1 {
			t.Fatalf("inventory %+v", inv)
		}
		if len(res.Changed) != 2 {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("equipment never stacks", func(t *testing.T) {
		h.add(t, "k4", "wooden_sword", 1)
		h.add(t, "k5", "wooden_sword", 1)
		inv := h.inventory(t)
		swords := 0
		for _, it := range inv {
			if it.TemplateID == "wooden_sword" {
				swords++
				if it.Quantity != 1 || it.Tab != gamedata.Equip {
					t.Fatalf("sword %+v", it)
				}
			}
		}
		if swords != 2 {
			t.Fatalf("%d sword rows, want 2", swords)
		}
	})
	t.Run("a full inventory gives inventory_full and writes nothing", func(t *testing.T) {
		for i := range SlotsPerTab - 2 { // the etc tab fills with 22 more gels
			h.add(t, "fill-"+string(rune('a'+i)), "slime_gel", 100)
		}
		h.add(t, "fill-x", "slime_gel", 100)
		h.add(t, "fill-y", "slime_gel", 100)
		items, events, outbox := h.count(t, `SELECT count(*) FROM items`), h.count(t, `SELECT count(*) FROM item_events`), h.count(t, `SELECT count(*) FROM outbox`)
		_, err := h.svc.Add(ctx, h.actor, AddInput{Key: "full", TemplateID: "slime_gel", Quantity: 1, Cause: Cause{Type: CauseDrop}})
		if !errors.Is(err, ErrInventoryFull) {
			t.Fatalf("err = %v", err)
		}
		if h.count(t, `SELECT count(*) FROM items`) != items || h.count(t, `SELECT count(*) FROM item_events`) != events || h.count(t, `SELECT count(*) FROM outbox`) != outbox {
			t.Fatal("a failed add wrote rows")
		}
		if h.count(t, `SELECT count(*) FROM idempotency_keys WHERE key = 'full'`) != 0 {
			t.Fatal("a failed add stored its idempotency key")
		}
	})
	t.Run("bad input", func(t *testing.T) {
		tests := []struct {
			name string
			in   AddInput
			want error
		}{
			{"unknown template", AddInput{Key: "b1", TemplateID: "excalibur", Quantity: 1}, ErrUnknownItem},
			{"zero quantity", AddInput{Key: "b2", TemplateID: "red_potion", Quantity: 0}, ErrBadQuantity},
			{"more than a stack", AddInput{Key: "b3", TemplateID: "red_potion", Quantity: 101}, ErrBadQuantity},
			{"two swords at once", AddInput{Key: "b4", TemplateID: "wooden_sword", Quantity: 2}, ErrBadQuantity},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := h.svc.Add(ctx, h.actor, tt.in); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		}
	})
}

func TestMove(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "a1", "red_potion", 60) // use slot 0
	h.add(t, "a2", "blue_potion", 10)
	inv := h.inventory(t)
	red, blue := inv[0], inv[1]
	move := func(key string, id model.ItemID, slot int) (Result, error) {
		return h.svc.Move(ctx, h.actor, MoveInput{Key: key, ItemID: id, Tab: gamedata.Use, Slot: slot})
	}

	t.Run("to an empty slot", func(t *testing.T) {
		res, err := move("m1", red.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changed) != 2 || res.Changed[0].Item != nil || res.Changed[0].Slot != 0 || res.Changed[1].Slot != 5 || res.Changed[1].Item.ID != red.ID {
			t.Fatalf("result %+v", res)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE item_id = $1 AND kind = 'moved' AND quantity_delta = 0`, red.ID) != 1 {
			t.Fatal("no moved event")
		}
	})
	t.Run("onto a different item is a conflict", func(t *testing.T) {
		_, err := move("m2", red.ID, 1) // blue is in slot 1
		if !errors.Is(err, ErrDifferentItem) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("merging into a matching stack", func(t *testing.T) {
		h.add(t, "a3", "red_potion", 50) // red at 5 has 60: 40 fit, 10 open slot 0
		inv := h.inventory(t)
		var second Item
		for _, it := range inv {
			if it.TemplateID == "red_potion" && it.ID != red.ID {
				second = it
			}
		}
		if second.Quantity != 10 {
			t.Fatalf("second red stack %+v", second)
		}
		// Make room: discard 20 from the big one (100 → 80), then merge the 10 in.
		if _, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d0", ItemID: red.ID, Quantity: 20}); err != nil {
			t.Fatal(err)
		}
		res, err := move("m3", second.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		inv = h.inventory(t)
		reds := 0
		for _, it := range inv {
			if it.TemplateID == "red_potion" {
				reds++
				if it.Quantity != 90 {
					t.Fatalf("merged stack %+v", it)
				}
			}
		}
		if reds != 1 || len(res.Changed) != 2 || res.Changed[0].Item != nil {
			t.Fatalf("reds %d result %+v", reds, res)
		}
		if h.count(t, `SELECT count(*) FROM items WHERE id = $1 AND location = 'destroyed' AND quantity = 0`, second.ID) != 1 {
			t.Fatal("the emptied source is not destroyed")
		}
	})
	t.Run("a partial merge leaves the rest in place", func(t *testing.T) {
		h.add(t, "a4", "red_potion", 30) // 10 fit into the 90, 20 open a slot
		inv := h.inventory(t)
		var rest Item
		for _, it := range inv {
			if it.TemplateID == "red_potion" && it.Quantity == 20 {
				rest = it
			}
		}
		if rest.ID.IsZero() {
			t.Fatalf("inventory %+v", inv)
		}
		if _, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d1", ItemID: red.ID, Quantity: 5}); err != nil { // 100 → 95: room for 5
			t.Fatal(err)
		}
		if _, err := move("m4", rest.ID, 5); err != nil {
			t.Fatal(err)
		}
		inv = h.inventory(t)
		for _, it := range inv {
			if it.ID == rest.ID && it.Quantity != 15 {
				t.Fatalf("rest %+v, want 15 left", it)
			}
			if it.ID == red.ID && it.Quantity != 100 {
				t.Fatalf("target %+v, want 100", it)
			}
		}
	})
	t.Run("bad moves", func(t *testing.T) {
		tests := []struct {
			name string
			in   MoveInput
			want error
		}{
			{"another tab", MoveInput{Key: "mb1", ItemID: blue.ID, Tab: gamedata.Etc, Slot: 2}, ErrWrongTab},
			{"slot out of range", MoveInput{Key: "mb2", ItemID: blue.ID, Tab: gamedata.Use, Slot: 24}, ErrBadSlot},
			{"negative slot", MoveInput{Key: "mb3", ItemID: blue.ID, Tab: gamedata.Use, Slot: -1}, ErrBadSlot},
			{"someone else's item", MoveInput{Key: "mb4", ItemID: model.NewItemID(), Tab: gamedata.Use, Slot: 2}, ErrNotFound},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := h.svc.Move(ctx, h.actor, tt.in); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
			})
		}
	})
	t.Run("someone else's item cannot be moved by another character", func(t *testing.T) {
		other := Actor{Account: model.NewAccountID(), Character: model.NewCharacterID()}
		_, err := h.svc.Move(ctx, other, MoveInput{Key: "x", ItemID: blue.ID, Tab: gamedata.Use, Slot: 9})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDiscard(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "a1", "red_potion", 40)
	h.add(t, "a2", "wooden_sword", 1)
	inv := h.inventory(t)
	var potion, sword Item
	for _, it := range inv {
		switch it.TemplateID {
		case "red_potion":
			potion = it
		case "wooden_sword":
			sword = it
		}
	}
	t.Run("part of a stack", func(t *testing.T) {
		res, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d1", ItemID: potion.ID, Quantity: 15})
		if err != nil || len(res.Changed) != 1 || res.Changed[0].Item.Quantity != 25 {
			t.Fatalf("result %+v err %v", res, err)
		}
	})
	t.Run("a whole item", func(t *testing.T) {
		res, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d2", ItemID: sword.ID, Quantity: 1})
		if err != nil || len(res.Changed) != 1 || res.Changed[0].Item != nil || res.Changed[0].Tab != gamedata.Equip {
			t.Fatalf("result %+v err %v", res, err)
		}
		if len(h.inventory(t)) != 1 {
			t.Fatal("the sword is still in the inventory")
		}
		if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemDiscarded'`) != 2 {
			t.Fatal("no ItemDiscarded outbox events")
		}
	})
	t.Run("more than held, zero, and someone else's", func(t *testing.T) {
		if _, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d3", ItemID: potion.ID, Quantity: 26}); !errors.Is(err, ErrBadQuantity) {
			t.Fatalf("err = %v", err)
		}
		if _, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d4", ItemID: potion.ID, Quantity: 0}); !errors.Is(err, ErrBadQuantity) {
			t.Fatalf("err = %v", err)
		}
		other := Actor{Account: model.NewAccountID(), Character: model.NewCharacterID()}
		if _, err := h.svc.Discard(ctx, other, DiscardInput{Key: "d5", ItemID: potion.ID, Quantity: 1}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
		if _, err := h.svc.Discard(ctx, h.actor, DiscardInput{Key: "d6", ItemID: sword.ID, Quantity: 1}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a destroyed item: %v", err)
		}
	})
}

func TestIdempotency(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	in := AddInput{Key: "pickup-1", TemplateID: "red_potion", Quantity: 3, Cause: Cause{Type: CauseDrop, ID: uuid.New()}}
	first, err := h.svc.Add(ctx, h.actor, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("the same key twice gives the same response and one effect", func(t *testing.T) {
		second, err := h.svc.Add(ctx, h.actor, in)
		if err != nil {
			t.Fatal(err)
		}
		if len(second.Changed) != 1 || second.Changed[0].Item.ID != first.Changed[0].Item.ID || second.Changed[0].Item.Quantity != 3 {
			t.Fatalf("replay %+v, first %+v", second, first)
		}
		inv := h.inventory(t)
		if len(inv) != 1 || inv[0].Quantity != 3 {
			t.Fatalf("inventory after the replay %+v", inv)
		}
		if h.count(t, `SELECT count(*) FROM item_events`) != 1 || h.count(t, `SELECT count(*) FROM outbox`) != 1 {
			t.Fatal("the replay wrote events")
		}
	})
	t.Run("the same key with a different request gives idempotency_key_reused", func(t *testing.T) {
		other := in
		other.Quantity = 4
		_, err := h.svc.Add(ctx, h.actor, other)
		if !errors.Is(err, ErrKeyReused) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("keys are scoped to the account", func(t *testing.T) {
		other := Actor{Account: model.NewAccountID(), Character: model.NewCharacterID()}
		if _, err := h.svc.Add(ctx, other, in); err != nil {
			t.Fatalf("another account with the same key: %v", err)
		}
	})
	t.Run("move and discard are idempotent too", func(t *testing.T) {
		inv := h.inventory(t)
		mv := MoveInput{Key: "mv", ItemID: inv[0].ID, Tab: gamedata.Use, Slot: 9}
		a, err := h.svc.Move(ctx, h.actor, mv)
		if err != nil {
			t.Fatal(err)
		}
		b, err := h.svc.Move(ctx, h.actor, mv)
		if err != nil || len(b.Changed) != len(a.Changed) || b.Changed[1].Slot != 9 {
			t.Fatalf("replayed move %+v err %v", b, err)
		}
		if h.inventory(t)[0].Slot != 9 {
			t.Fatal("the replayed move changed the slot again")
		}
		d := DiscardInput{Key: "dc", ItemID: inv[0].ID, Quantity: 1}
		if _, err := h.svc.Discard(ctx, h.actor, d); err != nil {
			t.Fatal(err)
		}
		if res, err := h.svc.Discard(ctx, h.actor, d); err != nil || res.Changed[0].Item.Quantity != 2 {
			t.Fatalf("replayed discard %+v err %v", res, err)
		}
		if h.inventory(t)[0].Quantity != 2 {
			t.Fatal("the replayed discard discarded again")
		}
	})
}

func TestStarterItemsJoinCharacterCreation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	chars := character.NewService(character.NewPostgres(h.pool, func(tx pgx.Tx) character.StarterItems {
		return h.svc.Within(h.store.UnitOfWork(tx))
	}), h.data, character.Options{})
	c, err := chars.Create(ctx, model.NewAccountID(), character.CreateInput{Name: "Ayla", Class: model.Warrior})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := h.svc.Inventory(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, it := range inv {
		got[it.TemplateID] += it.Quantity
	}
	if got["wooden_sword"] != 1 || got["red_potion"] != 5 || len(inv) != 2 {
		t.Fatalf("starter inventory %+v", inv)
	}
	if h.count(t, `SELECT count(*) FROM item_events WHERE cause_type = 'starter' AND to_owner = $1`, c.ID) != 2 {
		t.Fatal("starter events missing")
	}
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemGranted'`) != 2 {
		t.Fatal("ItemGranted outbox events missing")
	}
	t.Run("a bowman gets a bow", func(t *testing.T) {
		b, err := chars.Create(ctx, model.NewAccountID(), character.CreateInput{Name: "Robin", Class: model.Bowman})
		if err != nil {
			t.Fatal(err)
		}
		inv, _ := h.svc.Inventory(ctx, b.ID)
		if len(inv) != 2 || inv[0].TemplateID != "short_bow" {
			t.Fatalf("bowman inventory %+v", inv)
		}
	})
}

func TestOutboxRowsExistExactlyWhenTheChangeCommits(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	before := h.count(t, `SELECT count(*) FROM outbox`)
	h.add(t, "o1", "slime_gel", 2)
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemLooted'`) != before+1 {
		t.Fatal("no ItemLooted row after a committed add")
	}
	// A rolled-back transaction leaves no row: run the rule on a unit of
	// work and fail the transaction afterwards.
	err := h.store.InTx(ctx, func(uow UnitOfWork) error {
		tmpl, _ := h.data.Item("slime_gel")
		if _, err := h.svc.add(ctx, uow, h.actor.Character, tmpl, 1, Cause{Type: CauseDrop}); err != nil {
			return err
		}
		return errors.New("roll it back")
	})
	if err == nil {
		t.Fatal("no error")
	}
	if h.count(t, `SELECT count(*) FROM outbox`) != before+1 {
		t.Fatal("a rolled-back change left an outbox row")
	}
	if h.inventory(t)[0].Quantity != 2 {
		t.Fatal("a rolled-back change changed the inventory")
	}
}
