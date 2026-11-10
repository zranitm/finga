package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/itemtest"
)

func checkInvariants(t *testing.T, pool *pgxpool.Pool, data *gamedata.Data) {
	t.Helper()
	ctx := context.Background()
	mismatches, err := itemtest.CheckInvariants(ctx, pool, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) > 0 {
		t.Fatalf("invariants failed: %v", mismatches)
	}
}

func TestInvariantsHoldAfterTheServiceTests(t *testing.T) {
	h := newHarness(t)
	h.add(t, "i1", "red_potion", 70)
	h.add(t, "i2", "red_potion", 70)
	h.add(t, "i3", "wooden_sword", 1)
	var stack model.Item
	for _, it := range h.inventory(t) {
		if it.TemplateID == "red_potion" && it.Quantity == 100 {
			stack = it
		}
	}
	if _, err := h.svc.Discard(context.Background(), h.actor, application.DiscardInput{Key: "i4", ItemID: stack.ID, Quantity: 10}); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, h.pool, h.data)
}

func TestInvariantChecksDetectCorruption(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "c1", "red_potion", 10)
	inv := h.inventory(t)
	// Corrupt the quantity behind the module's back.
	if _, err := h.pool.Exec(ctx, `UPDATE items SET quantity = 11 WHERE id = $1`, inv[0].ID); err != nil {
		t.Fatal(err)
	}
	for range 5 { // the same order every run
		got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || !strings.HasPrefix(got[0], "5 ") || !strings.HasPrefix(got[1], "6 ") {
			t.Fatalf("found %v, want check 5's mismatch, then check 6's", got)
		}
	}
}

// Corruptions whose events still sum to each item's quantity pass check 5; check 6 sees them.
func TestCheckSixCatchesWhatCheckFiveCannot(t *testing.T) {
	ctx := context.Background()
	t.Run("a drop recorded as the player's own", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "c1", "red_potion", 10)
		if _, err := h.pool.Exec(ctx, `UPDATE item_events SET cause_type = 'player' WHERE kind = 'created'`); err != nil {
			t.Fatal(err)
		}
		got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], "6 template totals") {
			t.Fatalf("found %v, want check 6's template totals alone", got)
		}
	})
	t.Run("a merge whose split half is no longer the player's", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "c2", "red_potion", 100) // a full stack in slot 0
		h.add(t, "c3", "red_potion", 30)  // 30 more open slot 1
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "c4", ItemID: h.inventory(t)[0].ID, Quantity: 20}); err != nil {
			t.Fatal(err)
		}
		var small model.Item
		for _, it := range h.inventory(t) {
			if it.Quantity == 30 {
				small = it
			}
		}
		if _, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "c5", ItemID: small.ID, Tab: gamedata.Use, Slot: 0}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.pool.Exec(ctx, `UPDATE item_events SET cause_type = 'drop' WHERE kind = 'split'`); err != nil {
			t.Fatal(err)
		}
		got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], "6 stacks and splits") {
			t.Fatalf("found %v, want check 6's stacks and splits alone", got)
		}
	})
}

// Check 11 holds each equipment item to one roll, and anything else to none.
func TestCheckElevenCatchesBadRolls(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		corrupt string
	}{
		{"equipment without a roll", `DELETE FROM item_rolls`},
		{"a potion with a roll", `INSERT INTO item_rolls (item_id, physical_attack, physical_defense, str, dex, "int", agi, vit, accuracy, evasion, crit_rate, crit_damage, max_hp, max_mp, created_at, updated_at)
			SELECT id, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, now(), now() FROM items WHERE template_id = 'red_potion'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.add(t, "c1", "iron_sword", 1)
			h.add(t, "c2", "red_potion", 1)
			if _, err := h.pool.Exec(ctx, tt.corrupt); err != nil {
				t.Fatal(err)
			}
			got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], "11 ") {
				t.Fatalf("found %v, want check 11's mismatch alone", got)
			}
		})
	}
}

// Checks 9, 10 and 12 hold what is worn to one item per slot, a slot of its kind, and its equip events.
func TestTheWornChecksCatchCorruption(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		corrupt string
		want    []string
	}{
		{"two items worn in one slot", `ALTER TABLE items DROP CONSTRAINT one_item_per_equip_slot;
			UPDATE items SET location = 'equipped', tab = NULL, slot = NULL, equip_slot = 'weapon' WHERE template_id = 'iron_sword';
			INSERT INTO item_events (item_id, kind, quantity_delta, cause_type, occurred_at, created_at, updated_at)
				SELECT id, 'equipped', 0, 'player', now(), now(), now() FROM items WHERE template_id = 'iron_sword'`, []string{"9 "}},
		{"a sword worn as a hat", `UPDATE items SET equip_slot = 'hat' WHERE location = 'equipped'`, []string{"10 "}},
		{"worn with no equipped event", `DELETE FROM item_events WHERE kind = 'equipped'`, []string{"12 "}},
		{"in the inventory after an equipped event", `INSERT INTO item_events (item_id, kind, quantity_delta, cause_type, occurred_at, created_at, updated_at)
			SELECT id, 'equipped', 0, 'player', now(), now(), now() FROM items WHERE template_id = 'iron_sword'`, []string{"12 "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			sword := h.added(t, "wooden_sword")
			if _, err := h.equip(uuidv7.NewString(), sword.ID, "", 1); err != nil {
				t.Fatal(err)
			}
			h.added(t, "iron_sword")
			checkInvariants(t, h.pool, h.data)
			if _, err := h.pool.Exec(ctx, tt.corrupt); err != nil {
				t.Fatal(err)
			}
			got, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("found %v, want %v", got, tt.want)
			}
			for k, prefix := range tt.want {
				if !strings.HasPrefix(got[k], prefix) {
					t.Fatalf("found %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// A roll is what the item rolled when it dropped: a range retuned since never invalidates it.
func TestARollOutsideTodaysRangesStillHolds(t *testing.T) {
	h := newHarness(t)
	h.added(t, "iron_sword")
	if _, err := h.pool.Exec(context.Background(), `UPDATE item_rolls SET physical_attack = physical_attack + 100, str = 3`); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, h.pool, h.data)
}

func TestInventoryProperties(t *testing.T) {
	data, err := gamedata.Load("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	pool := cluster.DB(t)
	store := NewPostgres(pool, nil)
	svc := application.NewService(application.Deps{Store: store, Content: data, Logger: slog.New(slog.DiscardHandler)})
	templates := []string{"red_potion", "blue_potion", "slime_gel", "wooden_sword", "short_bow", "leather_cap", "leather_vest"}
	var equipSlots []string
	for _, s := range data.EquipSlots() {
		equipSlots = append(equipSlots, s.ID)
	}
	tabs := []gamedata.Tab{gamedata.Equip, gamedata.Use, gamedata.Etc}
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		actor := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		var added []application.AddInput // every drop tried, whose keys a batch may reuse
		steps := rapid.IntRange(1, 25).Draw(rt, "steps")
		for i := range steps {
			key := fmt.Sprintf("k%d", i)
			drop := func(key string) application.AddInput {
				tmpl := rapid.SampledFrom(templates).Draw(rt, "template")
				it, _ := data.Item(tmpl)
				qty := rapid.IntRange(1, it.MaxStack).Draw(rt, "qty")
				var stats *progression.ItemStats
				if it.Tab == gamedata.Equip {
					roll := progression.Rolled(it.Rolls, func(r gamedata.Range) int { return rapid.IntRange(r.Min, r.Max).Draw(rt, "stat") })
					stats = &roll
				}
				return application.AddInput{Key: key, TemplateID: tmpl, Quantity: qty, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: stats}
			}
			switch rapid.IntRange(0, 5).Draw(rt, "op") {
			case 0:
				in := drop(key)
				added = append(added, in)
				if _, err := svc.Add(ctx, actor, in); err != nil && !isRule(err) {
					rt.Fatalf("add: %v", err)
				}
			case 5:
				// New drops, and now and then one added before: the same again, which replays, or under other arguments, which is refused.
				var drops []application.AddInput
				for j := range rapid.IntRange(1, 4).Draw(rt, "drops") {
					in := drop(fmt.Sprintf("%s-%d", key, j))
					if len(added) > 0 && rapid.Bool().Draw(rt, "an old key") {
						in = rapid.SampledFrom(added).Draw(rt, "old drop")
						if rapid.Bool().Draw(rt, "other arguments") {
							in.Quantity++
						}
					}
					drops = append(drops, in)
				}
				added = append(added, drops...)
				b, err := svc.AddMany(ctx, actor, drops)
				if err != nil {
					rt.Fatalf("add many: %v", err)
				}
				for _, out := range b.Outcomes {
					if out.Err != nil && !isRule(out.Err) && !errors.Is(out.Err, model.ErrKeyReused) && !errors.Is(out.Err, model.ErrBadQuantity) {
						rt.Fatalf("a drop refused by %v", out.Err)
					}
				}
			case 1:
				read, _ := svc.Inventory(ctx, actor.Character)
				inv := read.Items
				if len(inv) == 0 {
					continue
				}
				src := rapid.SampledFrom(inv).Draw(rt, "item")
				tab := src.Tab
				if rapid.Bool().Draw(rt, "wrong tab") {
					tab = rapid.SampledFrom(tabs).Draw(rt, "tab")
				}
				slot := rapid.IntRange(0, model.SlotsPerTab-1).Draw(rt, "slot")
				_, err := svc.Move(ctx, actor, application.MoveInput{Key: key, ItemID: src.ID, Tab: tab, Slot: slot})
				if err != nil && !isRule(err) {
					rt.Fatalf("move: %v", err)
				}
			case 2:
				read, _ := svc.Inventory(ctx, actor.Character)
				inv := read.Items
				if len(inv) == 0 {
					continue
				}
				src := rapid.SampledFrom(inv).Draw(rt, "item")
				qty := rapid.IntRange(1, src.Quantity+1).Draw(rt, "qty")
				_, err := svc.Discard(ctx, actor, application.DiscardInput{Key: key, ItemID: src.ID, Quantity: qty})
				if err != nil && !isRule(err) {
					rt.Fatalf("discard: %v", err)
				}
			case 3:
				read, _ := svc.Inventory(ctx, actor.Character)
				if len(read.Items) == 0 {
					continue
				}
				src := rapid.SampledFrom(read.Items).Draw(rt, "item")
				_, err := svc.Equip(ctx, actor, application.EquipInput{
					Key: key, ItemID: src.ID, EquipSlot: rapid.SampledFrom(append([]string{""}, equipSlots...)).Draw(rt, "equip slot"),
					Level: rapid.IntRange(1, 30).Draw(rt, "level"), Class: rapid.SampledFrom([]kernel.Class{kernel.Warrior, kernel.Bowman}).Draw(rt, "class"),
				})
				if err != nil && !isRule(err) {
					rt.Fatalf("equip: %v", err)
				}
			case 4:
				in := application.UnequipInput{Key: key, EquipSlot: rapid.SampledFrom(equipSlots).Draw(rt, "equip slot")}
				if rapid.Bool().Draw(rt, "chosen slot") {
					to := rapid.IntRange(0, model.SlotsPerTab-1).Draw(rt, "to slot")
					in.ToSlot = &to
				}
				if _, err := svc.Unequip(ctx, actor, in); err != nil && !isRule(err) {
					rt.Fatalf("unequip: %v", err)
				}
			}
		}
		checkInvariants(t, pool, data)
		read, err := svc.Inventory(ctx, actor.Character)
		if err != nil {
			rt.Fatal(err)
		}
		inv := read.Items
		seen := map[string]bool{}
		for _, it := range inv {
			k := fmt.Sprintf("%s/%d", it.Tab, it.Slot)
			if it.Worn() {
				k = "worn/" + it.EquipSlot
			}
			if seen[k] || it.Quantity < 1 {
				rt.Fatalf("inventory %+v", inv)
			}
			seen[k] = true
		}
	})
}

// isRule reports whether err is a rule error, which random sequences are expected to hit.
func isRule(err error) bool {
	for _, rule := range []error{
		model.ErrInventoryFull, model.ErrBadQuantity, model.ErrBadSlot, model.ErrWrongTab, model.ErrDifferentItem, model.ErrStackFull, model.ErrNotFound,
		model.ErrSlotTaken, model.ErrItemWorn, model.ErrLevelTooLow, model.ErrWrongClass, model.ErrStatsTooLow, model.ErrWrongEquipSlot, model.ErrNotEquipment, model.ErrNothingWorn,
	} {
		if errorsIs(err, rule) {
			return true
		}
	}
	return false
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
