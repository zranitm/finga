package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// These rules are about transactions, so they run against real Postgres.

type harness struct {
	pool  *pgxpool.Pool
	store *Postgres
	svc   *application.Service
	data  *gamedata.Data
	actor model.Actor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	data, err := gamedata.Load("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	pool := cluster.DB(t)
	store := NewPostgres(pool)
	return &harness{pool: pool, store: store, svc: application.NewService(store, data, application.Options{}), data: data, actor: model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}}
}

func (h *harness) add(t *testing.T, key, template string, qty int) application.Result {
	t.Helper()
	res, err := h.svc.Add(context.Background(), h.actor, application.AddInput{Key: key, TemplateID: template, Quantity: qty, Cause: model.Cause{Type: model.CauseDrop, ID: uuid.New()}})
	if err != nil {
		t.Fatalf("add %s x%d: %v", template, qty, err)
	}
	return res
}

func (h *harness) inventory(t *testing.T) []model.Item {
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
		for i := range model.SlotsPerTab - 2 { // the etc tab fills with 22 more gels
			h.add(t, "fill-"+string(rune('a'+i)), "slime_gel", 100)
		}
		h.add(t, "fill-x", "slime_gel", 100)
		h.add(t, "fill-y", "slime_gel", 100)
		items, events, outbox := h.count(t, `SELECT count(*) FROM items`), h.count(t, `SELECT count(*) FROM item_events`), h.count(t, `SELECT count(*) FROM outbox`)
		_, err := h.svc.Add(ctx, h.actor, application.AddInput{Key: "full", TemplateID: "slime_gel", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop}})
		if !errors.Is(err, model.ErrInventoryFull) {
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
			in   application.AddInput
			want error
		}{
			{"unknown template", application.AddInput{Key: "b1", TemplateID: "excalibur", Quantity: 1}, model.ErrUnknownItem},
			{"zero quantity", application.AddInput{Key: "b2", TemplateID: "red_potion", Quantity: 0}, model.ErrBadQuantity},
			{"more than a stack", application.AddInput{Key: "b3", TemplateID: "red_potion", Quantity: 101}, model.ErrBadQuantity},
			{"two swords at once", application.AddInput{Key: "b4", TemplateID: "wooden_sword", Quantity: 2}, model.ErrBadQuantity},
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
	move := func(key string, id kernel.ItemID, slot int) (application.Result, error) {
		return h.svc.Move(ctx, h.actor, application.MoveInput{Key: key, ItemID: id, Tab: gamedata.Use, Slot: slot})
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
		if !errors.Is(err, model.ErrDifferentItem) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("merging into a matching stack", func(t *testing.T) {
		h.add(t, "a3", "red_potion", 50) // red at 5 has 60: 40 fit, 10 open slot 0
		inv := h.inventory(t)
		var second model.Item
		for _, it := range inv {
			if it.TemplateID == "red_potion" && it.ID != red.ID {
				second = it
			}
		}
		if second.Quantity != 10 {
			t.Fatalf("second red stack %+v", second)
		}
		// Make room: discard 20 from the big one (100 → 80), then merge the 10 in.
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d0", ItemID: red.ID, Quantity: 20}); err != nil {
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
		var rest model.Item
		for _, it := range inv {
			if it.TemplateID == "red_potion" && it.Quantity == 20 {
				rest = it
			}
		}
		if rest.ID.IsZero() {
			t.Fatalf("inventory %+v", inv)
		}
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d1", ItemID: red.ID, Quantity: 5}); err != nil { // 100 → 95: room for 5
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
			in   application.MoveInput
			want error
		}{
			{"another tab", application.MoveInput{Key: "mb1", ItemID: blue.ID, Tab: gamedata.Etc, Slot: 2}, model.ErrWrongTab},
			{"slot out of range", application.MoveInput{Key: "mb2", ItemID: blue.ID, Tab: gamedata.Use, Slot: 24}, model.ErrBadSlot},
			{"negative slot", application.MoveInput{Key: "mb3", ItemID: blue.ID, Tab: gamedata.Use, Slot: -1}, model.ErrBadSlot},
			{"someone else's item", application.MoveInput{Key: "mb4", ItemID: kernel.NewItemID(), Tab: gamedata.Use, Slot: 2}, model.ErrNotFound},
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
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		_, err := h.svc.Move(ctx, other, application.MoveInput{Key: "x", ItemID: blue.ID, Tab: gamedata.Use, Slot: 9})
		if !errors.Is(err, model.ErrNotFound) {
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
	var potion, sword model.Item
	for _, it := range inv {
		switch it.TemplateID {
		case "red_potion":
			potion = it
		case "wooden_sword":
			sword = it
		}
	}
	t.Run("part of a stack", func(t *testing.T) {
		res, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d1", ItemID: potion.ID, Quantity: 15})
		if err != nil || len(res.Changed) != 1 || res.Changed[0].Item.Quantity != 25 {
			t.Fatalf("result %+v err %v", res, err)
		}
	})
	t.Run("a whole item", func(t *testing.T) {
		res, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d2", ItemID: sword.ID, Quantity: 1})
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
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d3", ItemID: potion.ID, Quantity: 26}); !errors.Is(err, model.ErrBadQuantity) {
			t.Fatalf("err = %v", err)
		}
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d4", ItemID: potion.ID, Quantity: 0}); !errors.Is(err, model.ErrBadQuantity) {
			t.Fatalf("err = %v", err)
		}
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.Discard(ctx, other, application.DiscardInput{Key: "d5", ItemID: potion.ID, Quantity: 1}); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d6", ItemID: sword.ID, Quantity: 1}); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("a destroyed item: %v", err)
		}
	})
}

func TestIdempotency(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	in := application.AddInput{Key: "pickup-1", TemplateID: "red_potion", Quantity: 3, Cause: model.Cause{Type: model.CauseDrop, ID: uuid.New()}}
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
		if !errors.Is(err, model.ErrKeyReused) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("keys are scoped to the account", func(t *testing.T) {
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.Add(ctx, other, in); err != nil {
			t.Fatalf("another account with the same key: %v", err)
		}
	})
	t.Run("move and discard are idempotent too", func(t *testing.T) {
		inv := h.inventory(t)
		mv := application.MoveInput{Key: "mv", ItemID: inv[0].ID, Tab: gamedata.Use, Slot: 9}
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
		d := application.DiscardInput{Key: "dc", ItemID: inv[0].ID, Quantity: 1}
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

func TestGrantStarter(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	warrior := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
	first, err := h.svc.GrantStarter(ctx, warrior, kernel.Warrior)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := h.svc.Inventory(ctx, warrior.Character)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, it := range inv {
		got[it.TemplateID] += it.Quantity
	}
	if got["wooden_sword"] != 1 || got["red_potion"] != 5 || len(inv) != 2 || len(first.Changed) != 2 {
		t.Fatalf("starter inventory %+v, changed %+v", inv, first.Changed)
	}
	if h.count(t, `SELECT count(*) FROM item_events WHERE cause_type = 'starter' AND to_owner = $1`, warrior.Character) != 2 {
		t.Fatal("starter events missing")
	}
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemGranted'`) != 2 {
		t.Fatal("ItemGranted outbox events missing")
	}

	t.Run("a second grant for the same character changes nothing and returns the first one's slots", func(t *testing.T) {
		again, err := h.svc.GrantStarter(ctx, warrior, kernel.Warrior)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Changed) != len(first.Changed) || again.Changed[0].Item.ID != first.Changed[0].Item.ID {
			t.Fatalf("replay %+v, first %+v", again.Changed, first.Changed)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE to_owner = $1`, warrior.Character) != 2 || h.count(t, `SELECT count(*) FROM outbox`) != 2 {
			t.Fatal("the second grant wrote again")
		}
	})
	t.Run("another class for the same character reuses the key", func(t *testing.T) {
		if _, err := h.svc.GrantStarter(ctx, warrior, kernel.Bowman); !errors.Is(err, model.ErrKeyReused) {
			t.Fatalf("err = %v, want ErrKeyReused", err)
		}
	})
	t.Run("a bowman gets a bow", func(t *testing.T) {
		bowman := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.GrantStarter(ctx, bowman, kernel.Bowman); err != nil {
			t.Fatal(err)
		}
		inv, _ := h.svc.Inventory(ctx, bowman.Character)
		if len(inv) != 2 || inv[0].TemplateID != "short_bow" {
			t.Fatalf("bowman inventory %+v", inv)
		}
	})
	t.Run("a class the game data does not have is unknown_item and writes nothing", func(t *testing.T) {
		thief := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.GrantStarter(ctx, thief, kernel.Thief); !errors.Is(err, model.ErrUnknownItem) {
			t.Fatalf("err = %v, want ErrUnknownItem", err)
		}
		if h.count(t, `SELECT count(*) FROM items WHERE owner_id = $1`, thief.Character) != 0 {
			t.Fatal("items written")
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
	// A rolled-back transaction leaves no row: the add runs whole on the
	// unit of work, and the transaction fails afterwards.
	svc := application.NewService(rollingBack{h.store}, h.data, application.Options{})
	_, err := svc.Add(ctx, h.actor, application.AddInput{Key: "o2", TemplateID: "slime_gel", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuid.New()}})
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

// rollingBack is the real store, whose transactions fail once the work in
// them has succeeded, so they roll back.
type rollingBack struct{ *Postgres }

func (r rollingBack) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	return r.Postgres.InTx(ctx, func(uow application.UnitOfWork) error {
		if err := fn(uow); err != nil {
			return err
		}
		return errors.New("roll it back")
	})
}
