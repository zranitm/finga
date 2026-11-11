package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// added adds one of template and returns the item it made.
func (h *harness) added(t *testing.T, template string) model.Item {
	t.Helper()
	res := h.add(t, uuidv7.NewString(), template, 1)
	return *res.Changed[len(res.Changed)-1].Item
}

// equip has the harness's warrior wear id at level.
func (h *harness) equip(key string, id kernel.ItemID, slot string, level int) (application.Result, error) {
	return h.svc.Equip(context.Background(), h.actor, application.EquipInput{Key: key, ItemID: id, EquipSlot: slot, Level: level, Class: kernel.Warrior})
}

func (h *harness) unequip(key, slot string, to *int) (application.Result, error) {
	return h.svc.Unequip(context.Background(), h.actor, application.UnequipInput{Key: key, EquipSlot: slot, ToSlot: to})
}

// held is the harness's item id as the inventory now reads it.
func (h *harness) held(t *testing.T, id kernel.ItemID) model.Item {
	t.Helper()
	for _, it := range h.inventory(t) {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("item %s is not held", id)
	return model.Item{}
}

func wornIn(it model.Item, slot string) model.Item {
	it.Tab, it.Slot, it.EquipSlot = "", 0, slot
	return it
}

func heldIn(it model.Item, tab gamedata.Tab, slot int) model.Item {
	it.Tab, it.Slot, it.EquipSlot = tab, slot, ""
	return it
}

func wantResult(t *testing.T, got application.Result, changed []model.Slot, equipment []model.WornSlot, wearing []model.Item) {
	t.Helper()
	if !reflect.DeepEqual(got.Changed, changed) || !reflect.DeepEqual(got.Equipment, equipment) || !reflect.DeepEqual(got.Worn, wearing) || got.Revision == 0 {
		t.Fatalf("result\n changed %+v\n equipment %+v\n worn %+v\n revision %d\nwant\n changed %+v\n equipment %+v\n worn %+v", got.Changed, got.Equipment, got.Worn, got.Revision, changed, equipment, wearing)
	}
}

func TestEquip(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	first := h.added(t, "wooden_sword")
	second := h.added(t, "wooden_sword")

	t.Run("an empty slot just receives the item", func(t *testing.T) {
		before := h.revision(t)
		key := uuidv7.NewString()
		res, err := h.equip(key, first.ID, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		on := wornIn(first, "weapon")
		wantResult(t, res, []model.Slot{{Tab: gamedata.Equip, Slot: first.Slot}}, []model.WornSlot{{Slot: "weapon", Item: &on}}, []model.Item{on})
		if res.Revision != before+1 {
			t.Fatalf("revision %d, want %d", res.Revision, before+1)
		}
		if got := h.held(t, first.ID); !reflect.DeepEqual(got, on) {
			t.Fatalf("read back %+v, want %+v", got, on)
		}
		if h.count(t, `SELECT count(*) FROM item_events WHERE item_id = $1 AND kind = 'equipped' AND quantity_delta = 0 AND cause_type = 'player' AND cause_id = $2`, first.ID, uuid.MustParse(key)) != 1 {
			t.Fatal("no equipped event caused by the request")
		}
	})
	t.Run("a filled slot swaps: the worn item takes the slot the new one left", func(t *testing.T) {
		res, err := h.equip(uuidv7.NewString(), second.ID, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		off, on := heldIn(first, gamedata.Equip, second.Slot), wornIn(second, "weapon")
		wantResult(t, res, []model.Slot{{Tab: gamedata.Equip, Slot: second.Slot, Item: &off}}, []model.WornSlot{{Slot: "weapon", Item: &on}}, []model.Item{on})
		if h.count(t, `SELECT count(*) FROM item_events WHERE item_id = $1 AND kind = 'unequipped'`, first.ID) != 1 {
			t.Fatal("no unequipped event for the item swapped out")
		}
	})
	t.Run("what the item and the character must be", func(t *testing.T) {
		potion, hat, vest, bow := h.added(t, "red_potion"), h.added(t, "leather_cap"), h.added(t, "leather_vest"), h.added(t, "short_bow")
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		tests := []struct {
			name string
			who  model.Actor
			in   application.EquipInput
			want error
		}{
			{"someone else's item", other, application.EquipInput{ItemID: hat.ID, Level: 1, Class: kernel.Warrior}, model.ErrNotFound},
			{"an item that doesn't exist", h.actor, application.EquipInput{ItemID: kernel.NewItemID(), Level: 1, Class: kernel.Warrior}, model.ErrNotFound},
			{"a potion", h.actor, application.EquipInput{ItemID: potion.ID, Level: 1, Class: kernel.Warrior}, model.ErrNotEquipment},
			{"a hat in the weapon slot", h.actor, application.EquipInput{ItemID: hat.ID, EquipSlot: "weapon", Level: 1, Class: kernel.Warrior}, model.ErrWrongEquipSlot},
			{"a slot the game data doesn't have", h.actor, application.EquipInput{ItemID: hat.ID, EquipSlot: "boots", Level: 1, Class: kernel.Warrior}, model.ErrWrongEquipSlot},
			{"below the item's level", h.actor, application.EquipInput{ItemID: vest.ID, Level: 4, Class: kernel.Warrior}, model.ErrLevelTooLow},
			{"another class's item", h.actor, application.EquipInput{ItemID: bow.ID, Level: 1, Class: kernel.Warrior}, model.ErrWrongClass},
			{"an item already worn", h.actor, application.EquipInput{ItemID: second.ID, Level: 1, Class: kernel.Warrior}, model.ErrItemWorn},
		}
		for k, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				revision, events := h.revision(t), h.count(t, `SELECT count(*) FROM item_events`)
				tt.in.Key = fmt.Sprintf("r%d", k)
				if _, err := h.svc.Equip(ctx, tt.who, tt.in); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				if h.revision(t) != revision || h.count(t, `SELECT count(*) FROM item_events`) != events {
					t.Fatal("a refused equip wrote")
				}
			})
		}
		t.Run("at the item's level, for its class", func(t *testing.T) {
			if _, err := h.equip(uuidv7.NewString(), vest.ID, "top", 5); err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("an item's stat requirements, against the character's own stats", func(t *testing.T) {
		svc := application.NewService(application.Deps{Store: h.store, Content: requiring{h.data, "leather_vest", gamedata.Stats{STR: 20, VIT: 15}}, Logger: slog.New(slog.DiscardHandler)})
		vest := h.added(t, "leather_vest")
		equip := func(own gamedata.Stats) error {
			_, err := svc.Equip(ctx, h.actor, application.EquipInput{Key: uuidv7.NewString(), ItemID: vest.ID, Level: 5, Class: kernel.Warrior, Stats: own})
			return err
		}
		revision := h.revision(t)
		if err := equip(gamedata.Stats{STR: 20, VIT: 14}); !errors.Is(err, model.ErrVITTooLow) {
			t.Fatalf("VIT a point short: %v, want VIT too low", err)
		}
		if err := equip(gamedata.Stats{STR: 19, VIT: 14}); !errors.Is(err, model.ErrSTRTooLow) {
			t.Fatalf("STR and VIT short: %v, want STR too low, the first", err)
		}
		if h.revision(t) != revision {
			t.Fatal("a refused equip wrote")
		}
		if err := equip(gamedata.Stats{STR: 20, VIT: 15}); err != nil {
			t.Fatalf("exactly enough: %v", err)
		}
	})
	t.Run("a retry gets the stored reply, even after a level-up; other arguments under its key are refused", func(t *testing.T) {
		hat := h.added(t, "leather_cap")
		key := uuidv7.NewString()
		res, err := h.equip(key, hat.ID, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		again, err := h.equip(key, hat.ID, "", 2)
		if err != nil || !reflect.DeepEqual(again, res) {
			t.Fatalf("retry %+v, %v; want %+v", again, err, res)
		}
		if _, err := h.equip(key, hat.ID, "hat", 2); !errors.Is(err, model.ErrKeyReused) {
			t.Fatalf("err = %v, want ErrKeyReused", err)
		}
	})
}

func TestUnequip(t *testing.T) {
	h := newHarness(t)
	sword := h.added(t, "wooden_sword")
	wear := func(t *testing.T) {
		t.Helper()
		if _, err := h.equip(uuidv7.NewString(), sword.ID, "", 1); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("to the first free Equip-tab slot", func(t *testing.T) {
		wear(t)
		h.added(t, "leather_cap") // takes the slot the sword left
		res, err := h.unequip(uuidv7.NewString(), "weapon", nil)
		if err != nil {
			t.Fatal(err)
		}
		off := heldIn(sword, gamedata.Equip, 1)
		wantResult(t, res, []model.Slot{{Tab: gamedata.Equip, Slot: 1, Item: &off}}, []model.WornSlot{{Slot: "weapon"}}, nil)
		if h.count(t, `SELECT count(*) FROM item_events WHERE item_id = $1 AND kind = 'unequipped'`, sword.ID) != 1 {
			t.Fatal("no unequipped event")
		}
	})
	t.Run("to a chosen slot", func(t *testing.T) {
		wear(t)
		to := 7
		if _, err := h.unequip(uuidv7.NewString(), "weapon", &to); err != nil {
			t.Fatal(err)
		}
		if got := h.held(t, sword.ID); got.Worn() || got.Tab != gamedata.Equip || got.Slot != 7 {
			t.Fatalf("sword %+v, want it in slot 7", got)
		}
	})
	t.Run("what the slots must be", func(t *testing.T) {
		wear(t)
		taken, outside := 0, model.SlotsPerTab
		tests := []struct {
			name string
			slot string
			to   *int
			want error
		}{
			{"nothing worn there", "hat", nil, model.ErrNothingWorn},
			{"a slot the game data doesn't have", "boots", nil, model.ErrWrongEquipSlot},
			{"onto a taken slot", "weapon", &taken, model.ErrSlotTaken},
			{"outside the tab", "weapon", &outside, model.ErrBadSlot},
		}
		for k, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				revision := h.revision(t)
				if _, err := h.unequip(fmt.Sprintf("u%d", k), tt.slot, tt.to); !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				if h.revision(t) != revision {
					t.Fatal("a refused unequip wrote")
				}
			})
		}
	})
}

func TestAWornItemCannotBeMovedOrDiscarded(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	sword := h.added(t, "wooden_sword")
	if _, err := h.equip(uuidv7.NewString(), sword.ID, "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "m", ItemID: sword.ID, Tab: gamedata.Equip, Slot: 3}); !errors.Is(err, model.ErrItemWorn) {
		t.Fatalf("move: %v, want ErrItemWorn", err)
	}
	if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d", ItemID: sword.ID, Quantity: 1}); !errors.Is(err, model.ErrItemWorn) {
		t.Fatalf("discard: %v, want ErrItemWorn", err)
	}
	if !h.held(t, sword.ID).Worn() {
		t.Fatal("the sword came off")
	}
}

// fullEquipTab is a harness wearing a sword with all 24 Equip-tab slots holding swords.
func fullEquipTab(t *testing.T) (h *harness, wearing model.Item, tab []model.Item) {
	t.Helper()
	h = newHarness(t)
	wearing = h.added(t, "wooden_sword")
	if _, err := h.equip(uuidv7.NewString(), wearing.ID, "", 1); err != nil {
		t.Fatal(err)
	}
	for range model.SlotsPerTab {
		tab = append(tab, h.added(t, "wooden_sword"))
	}
	return h, wearing, tab
}

func TestAFullEquipTab(t *testing.T) {
	ctx := context.Background()
	pickUp := func(h *harness) error {
		_, err := h.svc.Add(ctx, h.actor, application.AddInput{Key: uuidv7.NewString(), TemplateID: "leather_cap", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: h.roll("leather_cap")})
		return err
	}

	t.Run("a swap works, the worn item taking exactly the slot the new one left; a pickup after it finds the tab still full", func(t *testing.T) {
		h, wearing, tab := fullEquipTab(t)
		next := tab[7]
		res, err := h.equip(uuidv7.NewString(), next.ID, "", 1)
		if err != nil {
			t.Fatal(err)
		}
		off, on := heldIn(wearing, gamedata.Equip, 7), wornIn(next, "weapon")
		wantResult(t, res, []model.Slot{{Tab: gamedata.Equip, Slot: 7, Item: &off}}, []model.WornSlot{{Slot: "weapon", Item: &on}}, []model.Item{on})
		items, events := h.count(t, `SELECT count(*) FROM items`), h.count(t, `SELECT count(*) FROM item_events`)
		if err := pickUp(h); !errors.Is(err, model.ErrEquipTabFull) {
			t.Fatalf("pickup: %v, want ErrEquipTabFull", err)
		}
		if h.count(t, `SELECT count(*) FROM items`) != items || h.count(t, `SELECT count(*) FROM item_events`) != events {
			t.Fatal("the refused pickup wrote")
		}
		if inv := h.inventory(t); len(inv) != model.SlotsPerTab+1 {
			t.Fatalf("%d items held, want the full tab and the worn sword", len(inv))
		}
		checkInvariants(t, h.pool, h.data)
	})
	t.Run("unequipping with nowhere to go is a full Equip tab", func(t *testing.T) {
		h, wearing, _ := fullEquipTab(t)
		if _, err := h.unequip(uuidv7.NewString(), "weapon", nil); !errors.Is(err, model.ErrEquipTabFull) {
			t.Fatalf("err = %v, want ErrEquipTabFull", err)
		}
		if !h.held(t, wearing.ID).Worn() {
			t.Fatal("the sword came off")
		}
	})
	t.Run("a swap leaves the one free slot free, for the pickup", func(t *testing.T) {
		h, wearing, tab := fullEquipTab(t)
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "d", ItemID: tab[3].ID, Quantity: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.equip(uuidv7.NewString(), tab[10].ID, "", 1); err != nil {
			t.Fatal(err)
		}
		if err := pickUp(h); err != nil {
			t.Fatal(err)
		}
		for _, it := range h.inventory(t) {
			if it.TemplateID == "leather_cap" && (it.Tab != gamedata.Equip || it.Slot != 3) || it.ID == wearing.ID && it.Slot != 10 {
				t.Fatalf("%+v; want the hat in slot 3 and the old sword in slot 10", it)
			}
		}
		checkInvariants(t, h.pool, h.data)
	})
}

// requiring is the game data with one item needing stats, which items.yaml may not have.
type requiring struct {
	*gamedata.Data
	item  string
	needs gamedata.Stats
}

func (r requiring) Item(id string) (gamedata.Item, bool) {
	it, ok := r.Data.Item(id)
	if id == r.item {
		it.Requires = r.needs
	}
	return it, ok
}
