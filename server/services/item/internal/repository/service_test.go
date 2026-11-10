package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/dbx"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/itemtest"
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
	store := NewPostgres(pool, nil)
	return &harness{pool: pool, store: store, svc: application.NewService(application.Deps{Store: store, Content: data, Logger: slog.New(slog.DiscardHandler)}), data: data, actor: model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}}
}

func (h *harness) add(t *testing.T, key, template string, qty int) application.Result {
	t.Helper()
	res, err := h.svc.Add(context.Background(), h.actor, application.AddInput{Key: key, TemplateID: template, Quantity: qty, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: h.roll(template)})
	if err != nil {
		t.Fatalf("add %s x%d: %v", template, qty, err)
	}
	return res
}

// roll is equipment's top roll, which a starter's lowest never equals; anything else has none.
func (h *harness) roll(template string) *progression.ItemStats {
	tmpl, ok := h.data.Item(template)
	if !ok || tmpl.Tab != gamedata.Equip {
		return nil
	}
	top := progression.Rolled(tmpl.Rolls, func(r gamedata.Range) int { return r.Max })
	return &top
}

func (h *harness) inventory(t *testing.T) []model.Item {
	t.Helper()
	inv, err := h.svc.Inventory(context.Background(), h.actor.Character)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Items
}

func (h *harness) revision(t *testing.T) uint64 {
	t.Helper()
	inv, err := h.svc.Inventory(context.Background(), h.actor.Character)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Revision
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

// rollingEverything is the game data with one item rolling every stat, which items.yaml may not have.
type rollingEverything struct {
	*gamedata.Data
	item string
}

func (r rollingEverything) Item(id string) (gamedata.Item, bool) {
	it, ok := r.Data.Item(id)
	if id == r.item {
		for _, f := range reflect.ValueOf(&it.Rolls).Elem().Fields() {
			f.Set(reflect.ValueOf(gamedata.Range{Min: 1, Max: 21}))
		}
	}
	return it, ok
}

func TestEveryStatOfARollIsStoredReadBackAndReplayedInItsOwnField(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.svc = application.NewService(application.Deps{Store: h.store, Content: rollingEverything{h.data, "iron_sword"}, Logger: slog.New(slog.DiscardHandler)})
	whole := application.AddInput{Key: "r-whole", TemplateID: "iron_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: &progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}}
	for _, try := range []string{"the add", "its replay"} {
		res, err := h.svc.Add(ctx, h.actor, whole)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Changed[0].Item; got.Stats == nil || *got.Stats != *whole.Stats {
			t.Fatalf("%s: %+v, want %+v", try, got.Stats, *whole.Stats)
		}
		if inv := h.inventory(t); len(inv) != 1 || inv[0].Stats == nil || *inv[0].Stats != *whole.Stats {
			t.Fatalf("after %s, read back %+v; want the one sword with %+v", try, inv, *whole.Stats)
		}
	}
}

func TestRolls(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	in := application.AddInput{Key: "r1", TemplateID: "iron_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: &progression.ItemStats{PhysicalAttack: 17}}
	rolled := func(t *testing.T, what string, it *model.Item) {
		t.Helper()
		if it == nil || it.Stats == nil || *it.Stats != *in.Stats {
			t.Fatalf("%s: %+v, want the sword's roll %+v", what, it, *in.Stats)
		}
	}
	first, err := h.svc.Add(ctx, h.actor, in)
	if err != nil {
		t.Fatal(err)
	}
	sword := *first.Changed[0].Item

	t.Run("equipment keeps the roll it was added with", func(t *testing.T) {
		rolled(t, "the added sword", &sword)
		inv := h.inventory(t)
		rolled(t, "the sword read back", &inv[0])
		if h.count(t, `SELECT count(*) FROM item_rolls WHERE item_id = $1 AND physical_attack = 17`, sword.ID) != 1 {
			t.Fatal("no roll row")
		}
	})
	t.Run("a retry replays the roll, and another roll under the key is refused", func(t *testing.T) {
		again, err := h.svc.Add(ctx, h.actor, in)
		if err != nil {
			t.Fatal(err)
		}
		rolled(t, "the replayed sword", again.Changed[0].Item)
		other := in
		other.Stats = &progression.ItemStats{PhysicalAttack: 18}
		if _, err := h.svc.Add(ctx, h.actor, other); !errors.Is(err, model.ErrKeyReused) {
			t.Fatalf("err = %v, want ErrKeyReused", err)
		}
	})
	t.Run("a moved sword keeps its roll, replayed too", func(t *testing.T) {
		mv := application.MoveInput{Key: "r2", ItemID: sword.ID, Tab: gamedata.Equip, Slot: 9}
		for _, try := range []string{"the move", "its replay"} {
			res, err := h.svc.Move(ctx, h.actor, mv)
			if err != nil {
				t.Fatal(err)
			}
			rolled(t, try, res.Changed[1].Item)
		}
	})
	t.Run("equipment an older build wrote with no roll reads as its lowest, and the owner's next change writes it", func(t *testing.T) {
		if _, err := h.pool.Exec(ctx, `DELETE FROM item_rolls WHERE item_id = $1`, sword.ID); err != nil {
			t.Fatal(err)
		}
		lowest := progression.ItemStats{PhysicalAttack: 16}
		if inv := h.inventory(t); inv[0].Stats == nil || *inv[0].Stats != lowest {
			t.Fatalf("read %+v, want the lowest roll %+v", inv[0].Stats, lowest)
		}
		if h.count(t, `SELECT count(*) FROM item_rolls WHERE item_id = $1`, sword.ID) != 0 {
			t.Fatal("a read wrote the roll")
		}
		var logs bytes.Buffer
		svc := application.NewService(application.Deps{Store: h.store, Content: h.data, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		res, err := svc.Move(ctx, h.actor, application.MoveInput{Key: "r4", ItemID: sword.ID, Tab: gamedata.Equip, Slot: 3})
		if err != nil {
			t.Fatal(err)
		}
		if it := res.Changed[1].Item; it.Stats == nil || *it.Stats != lowest {
			t.Fatalf("moved %+v, want the lowest roll", it.Stats)
		}
		if h.count(t, `SELECT count(*) FROM item_rolls WHERE item_id = $1 AND physical_attack = 16`, sword.ID) != 1 {
			t.Fatal("the change wrote no roll")
		}
		if !bytes.Contains(logs.Bytes(), []byte(`"level":"WARN","msg":"equipment had no roll; it now has its template's lowest"`)) || !bytes.Contains(logs.Bytes(), []byte(sword.ID.String())) {
			t.Fatalf("logged %s", logs.String())
		}
		checkInvariants(t, h.pool, h.data)
	})
	t.Run("anything but equipment has no roll", func(t *testing.T) {
		res := h.add(t, "r3", "red_potion", 3)
		if it := res.Changed[0].Item; it.Stats != nil {
			t.Fatalf("potion %+v", it)
		}
	})
	t.Run("bad rolls write nothing", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			stats    *progression.ItemStats
		}{
			{"equipment without a roll", "iron_sword", nil},
			{"a stat above its range", "iron_sword", &progression.ItemStats{PhysicalAttack: 21}},
			{"a stat below its range", "iron_sword", &progression.ItemStats{PhysicalAttack: 15}},
			{"a stat the template doesn't give", "iron_sword", &progression.ItemStats{PhysicalAttack: 17, STR: 1}},
			{"a potion with a roll", "red_potion", &progression.ItemStats{}},
		}
		for k, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				items, rolls := h.count(t, `SELECT count(*) FROM items`), h.count(t, `SELECT count(*) FROM item_rolls`)
				key := fmt.Sprintf("bad-%d", k)
				_, err := h.svc.Add(ctx, h.actor, application.AddInput{Key: key, TemplateID: tt.template, Quantity: 1, Cause: model.Cause{Type: model.CauseDrop}, Stats: tt.stats})
				if !errors.Is(err, model.ErrBadRoll) {
					t.Fatalf("err = %v, want ErrBadRoll", err)
				}
				if h.count(t, `SELECT count(*) FROM items`) != items || h.count(t, `SELECT count(*) FROM item_rolls`) != rolls || h.count(t, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, key) != 0 {
					t.Fatal("a refused roll wrote rows")
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
	t.Run("onto a full stack of the same item is stack_full, not a different item", func(t *testing.T) {
		var rest model.Item
		for _, it := range h.inventory(t) {
			if it.TemplateID == "red_potion" && it.ID != red.ID {
				rest = it
			}
		}
		if _, err := move("m5", rest.ID, 5); !errors.Is(err, model.ErrStackFull) {
			t.Fatalf("err = %v, want ErrStackFull", err)
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
	in := application.AddInput{Key: "pickup-1", TemplateID: "red_potion", Quantity: 3, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}}
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
	read, err := h.svc.Inventory(ctx, warrior.Character)
	if err != nil {
		t.Fatal(err)
	}
	inv := read.Items
	if len(inv) != 2 || inv[0].TemplateID != "red_potion" || inv[0].Quantity != 5 || inv[0].Tab != gamedata.Use || inv[1].TemplateID != "wooden_sword" || inv[1].EquipSlot != "weapon" {
		t.Fatalf("starter inventory %+v; want 5 potions in the Use tab and the sword worn", inv)
	}
	sword, _ := h.data.Item("wooden_sword")
	if lowest := progression.Lowest(sword.Rolls); inv[1].Stats == nil || *inv[1].Stats != lowest {
		t.Fatalf("starter sword rolled %+v, want the lowest roll %+v", inv[1].Stats, lowest)
	}
	worn := inv[1]
	if len(first.Changed) != 1 || first.Changed[0].Item.TemplateID != "red_potion" ||
		!reflect.DeepEqual(first.Equipment, []model.WornSlot{{Slot: "weapon", Item: &worn}}) || first.Revision != 1 {
		t.Fatalf("grant changed %+v and %+v at revision %d; want the potions' slot and the sword in the weapon slot, at 1", first.Changed, first.Equipment, first.Revision)
	}
	// Created, then equipped: the sword's events say it is worn, so check 12 holds.
	if h.count(t, `SELECT count(*) FROM item_events WHERE cause_type = 'starter' AND to_owner = $1`, warrior.Character) != 3 ||
		h.count(t, `SELECT count(*) FROM item_events WHERE kind = 'equipped' AND item_id = $1`, worn.ID) != 1 {
		t.Fatal("starter events missing")
	}
	if h.count(t, `SELECT count(*) FROM outbox WHERE type = 'ItemGranted'`) != 2 {
		t.Fatal("ItemGranted outbox events missing")
	}
	if mismatches, err := itemtest.CheckInvariants(ctx, h.pool, h.data); err != nil || len(mismatches) > 0 {
		t.Fatalf("invariants: %v %v", mismatches, err)
	}

	unchanged := func(t *testing.T, again application.Result, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Changed) != 0 || len(again.Equipment) != 0 {
			t.Fatalf("a second grant changed %+v and %+v", again.Changed, again.Equipment)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE to_owner = $1`, warrior.Character) != 3 || h.count(t, `SELECT count(*) FROM outbox`) != 2 {
			t.Fatal("the second grant wrote again")
		}
	}
	t.Run("a second grant for the same character changes nothing", func(t *testing.T) {
		again, err := h.svc.GrantStarter(ctx, warrior, kernel.Warrior)
		unchanged(t, again, err)
	})
	t.Run("nor does one for another class", func(t *testing.T) {
		again, err := h.svc.GrantStarter(ctx, warrior, kernel.Bowman)
		unchanged(t, again, err)
	})
	t.Run("the grant is recorded for good, not only while its idempotency key is kept", func(t *testing.T) {
		if _, err := h.pool.Exec(ctx, `DELETE FROM idempotency_keys`); err != nil {
			t.Fatal(err)
		}
		again, err := h.svc.GrantStarter(ctx, warrior, kernel.Warrior)
		unchanged(t, again, err)
	})
	t.Run("concurrent grants give one kit", func(t *testing.T) {
		mage := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				if _, err := h.svc.GrantStarter(ctx, mage, kernel.Warrior); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if n := h.count(t, `SELECT count(*) FROM item_events WHERE to_owner = $1`, mage.Character); n != 3 {
			t.Fatalf("%d starter events, want one kit's 3", n)
		}
	})
	t.Run("a bowman gets a bow", func(t *testing.T) {
		bowman := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.GrantStarter(ctx, bowman, kernel.Bowman); err != nil {
			t.Fatal(err)
		}
		read, _ := h.svc.Inventory(ctx, bowman.Character)
		if inv := read.Items; len(inv) != 2 || inv[1].TemplateID != "short_bow" || inv[1].EquipSlot != "weapon" {
			t.Fatalf("bowman inventory %+v", inv)
		}
	})
	// A grant that comes late, from the world's safety net, may find a weapon already worn.
	t.Run("starter equipment whose slot is taken lands in the inventory", func(t *testing.T) {
		late := newHarness(t)
		late.actor = model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		bow := late.added(t, "short_bow")
		if _, err := late.svc.Equip(ctx, late.actor, application.EquipInput{Key: uuidv7.NewString(), ItemID: bow.ID, Level: 1, Class: kernel.Bowman}); err != nil {
			t.Fatal(err)
		}
		kit, err := late.svc.GrantStarter(ctx, late.actor, kernel.Bowman)
		if err != nil {
			t.Fatal(err)
		}
		inv := late.inventory(t)
		if len(kit.Equipment) != 0 || len(kit.Changed) != 2 || len(inv) != 3 || inv[0].TemplateID != "short_bow" || inv[0].Tab != gamedata.Equip || inv[2].ID != bow.ID || inv[2].EquipSlot != "weapon" {
			t.Fatalf("late grant changed %+v and %+v, inventory %+v; want the starter bow in the Equip tab, the worn one kept", kit.Changed, kit.Equipment, inv)
		}
		if mismatches, err := itemtest.CheckInvariants(ctx, late.pool, late.data); err != nil || len(mismatches) > 0 {
			t.Fatalf("invariants: %v %v", mismatches, err)
		}
	})
	t.Run("a class the game data does not have is unknown_class and writes nothing", func(t *testing.T) {
		thief := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		if _, err := h.svc.GrantStarter(ctx, thief, kernel.Thief); !errors.Is(err, kernel.ErrUnknownClass) {
			t.Fatalf("err = %v, want ErrUnknownClass", err)
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
	// A transaction that fails after the add succeeds leaves no row.
	svc := application.NewService(application.Deps{Store: rollingBack{h.store}, Content: h.data, Logger: slog.New(slog.DiscardHandler)})
	_, err := svc.Add(ctx, h.actor, application.AddInput{Key: "o2", TemplateID: "slime_gel", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}})
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

// An inventory lock held past lock_timeout, through every rerun, is transient.ErrUnavailable, which callers retry.
func TestAnInventoryLockedPastTheTimeoutIsUnavailable(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	pool, err := dbx.NewPool(ctx, dbx.Config{URL: cluster.URL(t), MaxConns: 2, LockTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgres(pool, nil)
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- store.InTx(ctx, func(uow application.UnitOfWork) error {
			if _, err := uow.Items().LockInventory(ctx, h.actor.Character); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	_, err = application.NewService(application.Deps{Store: store, Content: h.data, Logger: slog.New(slog.DiscardHandler)}).Add(ctx, h.actor, application.AddInput{Key: "late", TemplateID: "red_potion", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop}})
	close(release)
	if !errors.Is(err, transient.ErrUnavailable) {
		t.Fatalf("err = %v, want transient.ErrUnavailable", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// rollingBack is the real store with transactions that fail after their work succeeds.
type rollingBack struct{ *Postgres }

func (r rollingBack) InTx(ctx context.Context, fn func(uow application.UnitOfWork) error) error {
	return r.Postgres.InTx(ctx, func(uow application.UnitOfWork) error {
		if err := fn(uow); err != nil {
			return err
		}
		return errors.New("roll it back")
	})
}

// Each outbox row carries its own slot's change, signed as its event's, so the rows add up to what changed.
func TestEachOutboxRowCarriesItsSlotsChange(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "seed", "red_potion", 60)
	h.add(t, "more", "red_potion", 70) // +40 onto the stack of 60, and a new stack of 30
	first := h.inventory(t)[0]
	if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "less", ItemID: first.ID, Quantity: 25}); err != nil {
		t.Fatal(err)
	}
	rows, err := h.pool.Query(ctx, `
		SELECT (convert_from(o.payload, 'UTF8')::jsonb->>'quantity')::int,
		       (SELECT sum(e.quantity_delta) FROM item_events e WHERE e.item_id::text = o.key AND e.occurred_at = o.occurred_at)
		FROM outbox o ORDER BY o.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][2]int
	for rows.Next() {
		var q, delta int
		if err := rows.Scan(&q, &delta); err != nil {
			t.Fatal(err)
		}
		got = append(got, [2]int{q, delta})
	}
	want := [][2]int{{60, 60}, {40, 40}, {30, 30}, {-25, -25}}
	if !slices.Equal(got, want) {
		t.Fatalf("outbox quantity and its event's delta per row: %v, want %v", got, want)
	}
}

// Every row a command writes carries one stamp, its transaction's time; a later change moves only updated_at.
func TestACommandStampsItsRowsAlike(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "seed", "red_potion", 5)
	stamps := func() int {
		t.Helper()
		return h.count(t, `SELECT count(DISTINCT s) FROM (
			SELECT created_at FROM items UNION ALL SELECT updated_at FROM items
			UNION ALL SELECT created_at FROM item_events UNION ALL SELECT updated_at FROM item_events
			UNION ALL SELECT created_at FROM outbox UNION ALL SELECT updated_at FROM outbox
			UNION ALL SELECT created_at FROM idempotency_keys UNION ALL SELECT updated_at FROM idempotency_keys) AS t(s)`)
	}
	if n := stamps(); n != 1 {
		t.Fatalf("one add wrote %d distinct stamps, want 1", n)
	}
	it := h.inventory(t)[0]
	if _, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "move", ItemID: it.ID, Tab: it.Tab, Slot: it.Slot + 1}); err != nil {
		t.Fatal(err)
	}
	if h.count(t, `SELECT count(*) FROM items WHERE updated_at > created_at`) != 1 {
		t.Fatal("the move left the item's updated_at")
	}
}
