package handler_test

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentineltest"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/model"
)

// fakeItems records what the handler asked and answers with result, or err.
type fakeItems struct {
	mu        sync.Mutex
	owner     kernel.CharacterID
	actor     model.Actor
	add       application.AddInput
	addMany   []application.AddInput
	batch     application.Batch
	move      application.MoveInput
	discard   application.DiscardInput
	use       application.UseInput
	equip     application.EquipInput
	unequip   application.UnequipInput
	handIn    application.HandInInput
	asked     []application.Completion
	handedIn  []application.HandIn
	class     kernel.Class
	inventory model.Inventory
	result    application.Result
	replayed  bool // Use answers it
	err       error
	calls     int
}

func (f *fakeItems) record() { f.calls++ }

func (f *fakeItems) Inventory(_ context.Context, owner kernel.CharacterID) (model.Inventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.owner = owner
	return f.inventory, f.err
}

func (f *fakeItems) Add(_ context.Context, actor model.Actor, in application.AddInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.add = actor, in
	return f.result, f.err
}

func (f *fakeItems) AddMany(_ context.Context, actor model.Actor, drops []application.AddInput) (application.Batch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.addMany = actor, drops
	return f.batch, f.err
}

func (f *fakeItems) Move(_ context.Context, actor model.Actor, in application.MoveInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.move = actor, in
	return f.result, f.err
}

func (f *fakeItems) Discard(_ context.Context, actor model.Actor, in application.DiscardInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.discard = actor, in
	return f.result, f.err
}

func (f *fakeItems) Use(_ context.Context, actor model.Actor, in application.UseInput) (application.Result, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.use = actor, in
	return f.result, f.replayed, f.err
}

func (f *fakeItems) Equip(_ context.Context, actor model.Actor, in application.EquipInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.equip = actor, in
	return f.result, f.err
}

func (f *fakeItems) Unequip(_ context.Context, actor model.Actor, in application.UnequipInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.unequip = actor, in
	return f.result, f.err
}

func (f *fakeItems) HandIn(_ context.Context, actor model.Actor, in application.HandInInput) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.handIn = actor, in
	return f.result, f.err
}

func (f *fakeItems) HandedIn(_ context.Context, owner kernel.CharacterID, asked []application.Completion) ([]application.HandIn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.owner, f.asked = owner, asked
	return f.handedIn, f.err
}

func (f *fakeItems) GrantStarter(_ context.Context, actor model.Actor, class kernel.Class) (application.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record()
	f.actor, f.class = actor, class
	return f.result, f.err
}

func client(t *testing.T, f *fakeItems) itemv1.ItemServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	itemv1.RegisterItemServiceServer(srv, handler.New(f))
	return itemv1.NewItemServiceClient(grpctest.Conn(t, srv))
}

var (
	account   = kernel.NewAccountID()
	character = kernel.NewCharacterID()
	actor     = &itemv1.Actor{AccountId: account.String(), CharacterId: character.String()}
	potion    = model.Item{ID: kernel.NewItemID(), TemplateID: "red_potion", Owner: character, Tab: gamedata.Use, Slot: 3, Quantity: 5}
	key1      = "k-" + uuidv7.NewString() // as the client sends one
	key2      = "k-" + uuidv7.NewString()
	// changed is a move's result: slot 0 emptied, slot 3 holds the potion, at the inventory's twelfth change.
	changed = application.Result{Changed: []model.Slot{{Tab: gamedata.Use, Slot: 0}, {Tab: gamedata.Use, Slot: 3, Item: &potion}}, Revision: 12}
)

type changedResponse interface {
	GetChanged() []*itemv1.Slot
	GetRevision() uint64
}

func wantChanged(t *testing.T, res changedResponse) {
	t.Helper()
	if res.GetRevision() != 12 {
		t.Fatalf("revision %d, want 12", res.GetRevision())
	}
	got := res.GetChanged()
	if len(got) != 2 || got[0].GetItem() != nil || got[0].GetTab() != "use" || got[0].GetSlot() != 0 {
		t.Fatalf("emptied slot %+v", got)
	}
	it := got[1].GetItem()
	if got[1].GetSlot() != 3 || it.GetId() != potion.ID.String() || it.GetTemplateId() != "red_potion" || it.GetTab() != "use" || it.GetSlot() != 3 || it.GetQuantity() != 5 {
		t.Fatalf("filled slot %+v", got[1])
	}
}

func TestEachCallMapsItsFieldsAndResult(t *testing.T) {
	ctx := context.Background()
	f := &fakeItems{inventory: model.Inventory{Revision: 7, Items: []model.Item{potion}}, result: changed}
	c := client(t, f)

	t.Run("Inventory", func(t *testing.T) {
		res, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: character.String()})
		if err != nil {
			t.Fatal(err)
		}
		if f.owner != character || len(res.GetItems()) != 1 || res.GetItems()[0].GetId() != potion.ID.String() || res.GetItems()[0].GetQuantity() != 5 || res.GetRevision() != 7 {
			t.Fatalf("owner %v items %+v revision %d", f.owner, res.GetItems(), res.GetRevision())
		}
	})
	t.Run("Add, a pickup keyed by its drop", func(t *testing.T) {
		drop := uuidv7.New()
		res, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: drop.String(), TemplateId: "slime_gel", Quantity: 2,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: drop.String()}})
		if err != nil {
			t.Fatal(err)
		}
		want := application.AddInput{Key: drop.String(), TemplateID: "slime_gel", Quantity: 2, Cause: model.Cause{Type: model.CauseDrop, ID: drop}}
		if f.add != want || f.actor != (model.Actor{Account: account, Character: character}) {
			t.Fatalf("add %+v actor %+v", f.add, f.actor)
		}
		wantChanged(t, res)
	})
	t.Run("Add, equipment with its roll, every stat in its own field", func(t *testing.T) {
		sword := model.Item{ID: kernel.NewItemID(), TemplateID: "iron_sword", Owner: character, Tab: gamedata.Equip, Slot: 2, Quantity: 1, Stats: &progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}}
		roll := &itemv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}
		f.result = application.Result{Changed: []model.Slot{{Tab: gamedata.Equip, Slot: 2, Item: &sword}}, Revision: 13}
		defer func() { f.result = changed }()
		res, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "iron_sword", Quantity: 1,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP}, Stats: roll})
		if err != nil {
			t.Fatal(err)
		}
		if f.add.Stats == nil || *f.add.Stats != *sword.Stats {
			t.Fatalf("roll sent %+v, want %+v", f.add.Stats, *sword.Stats)
		}
		if got := res.GetChanged()[0].GetItem().GetStats(); !proto.Equal(got, roll) {
			t.Fatalf("roll answered %v, want %v", got, roll)
		}
	})
	t.Run("Add without a cause ID", func(t *testing.T) {
		if _, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1,
			Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_PLAYER}}); err != nil {
			t.Fatal(err)
		}
		if f.add.Cause != (model.Cause{Type: model.CausePlayer}) {
			t.Fatalf("cause %+v", f.add.Cause)
		}
	})
	t.Run("Move", func(t *testing.T) {
		res, err := c.Move(ctx, &itemv1.MoveRequest{Actor: actor, IdempotencyKey: key1, ItemId: potion.ID.String(), Tab: "use", Slot: 3})
		if err != nil {
			t.Fatal(err)
		}
		if f.move != (application.MoveInput{Key: key1, ItemID: potion.ID, Tab: gamedata.Use, Slot: 3}) {
			t.Fatalf("move %+v", f.move)
		}
		wantChanged(t, res)
	})
	t.Run("Discard", func(t *testing.T) {
		res, err := c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, IdempotencyKey: key2, ItemId: potion.ID.String(), Quantity: 2})
		if err != nil {
			t.Fatal(err)
		}
		if f.discard != (application.DiscardInput{Key: key2, ItemID: potion.ID, Quantity: 2}) {
			t.Fatalf("discard %+v", f.discard)
		}
		wantChanged(t, res)
	})
	t.Run("Use, saying when an earlier call took it", func(t *testing.T) {
		f.replayed = true
		defer func() { f.replayed = false }()
		res, err := c.Use(ctx, &itemv1.UseRequest{Actor: actor, IdempotencyKey: key2, ItemId: potion.ID.String(), TemplateId: "red_potion"})
		if err != nil {
			t.Fatal(err)
		}
		if f.use != (application.UseInput{Key: key2, ItemID: potion.ID, Template: "red_potion"}) || !res.GetReplayed() {
			t.Fatalf("use %+v, replayed %v", f.use, res.GetReplayed())
		}
		wantChanged(t, res)
	})
	t.Run("Equip, answering what is worn", func(t *testing.T) {
		sword := model.Item{ID: kernel.NewItemID(), TemplateID: "iron_sword", Owner: character, Quantity: 1, Stats: &progression.ItemStats{PhysicalAttack: 17}, EquipSlot: "weapon"}
		f.result = application.Result{Changed: []model.Slot{{Tab: gamedata.Equip, Slot: 4}}, Equipment: []model.WornSlot{{Slot: "weapon", Item: &sword}}, Worn: []model.Item{sword}, Revision: 14}
		defer func() { f.result = changed }()
		res, err := c.Equip(ctx, &itemv1.EquipRequest{Actor: actor, IdempotencyKey: key1, ItemId: sword.ID.String(), EquipSlot: "weapon", Level: 9, Class: "warrior",
			Stats: &itemv1.PrimaryStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5}})
		if err != nil {
			t.Fatal(err)
		}
		if f.equip != (application.EquipInput{Key: key1, ItemID: sword.ID, EquipSlot: "weapon", Level: 9, Class: kernel.Warrior, Stats: gamedata.Stats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5}}) {
			t.Fatalf("equip %+v", f.equip)
		}
		eq, worn := res.GetEquipment(), res.GetWorn()
		if len(res.GetChanged()) != 1 || res.GetChanged()[0].GetItem() != nil || res.GetChanged()[0].GetSlot() != 4 || res.GetRevision() != 14 {
			t.Fatalf("changed %+v revision %d", res.GetChanged(), res.GetRevision())
		}
		if len(eq) != 1 || eq[0].GetEquipSlot() != "weapon" || eq[0].GetItem().GetId() != sword.ID.String() || eq[0].GetItem().GetEquipSlot() != "weapon" || eq[0].GetItem().GetStats().GetPhysicalAttack() != 17 {
			t.Fatalf("equipment %+v", eq)
		}
		if len(worn) != 1 || worn[0].GetId() != sword.ID.String() || worn[0].GetEquipSlot() != "weapon" {
			t.Fatalf("worn %+v", worn)
		}
	})
	t.Run("Unequip, to the first free slot or a chosen one", func(t *testing.T) {
		if _, err := c.Unequip(ctx, &itemv1.UnequipRequest{Actor: actor, IdempotencyKey: key1, EquipSlot: "hat"}); err != nil {
			t.Fatal(err)
		}
		if f.unequip.Key != key1 || f.unequip.EquipSlot != "hat" || f.unequip.ToSlot != nil {
			t.Fatalf("unequip %+v", f.unequip)
		}
		to := int32(6)
		res, err := c.Unequip(ctx, &itemv1.UnequipRequest{Actor: actor, IdempotencyKey: key2, EquipSlot: "hat", ToSlot: &to})
		if err != nil {
			t.Fatal(err)
		}
		if f.unequip.ToSlot == nil || *f.unequip.ToSlot != 6 {
			t.Fatalf("unequip %+v", f.unequip)
		}
		wantChanged(t, res)
	})
	t.Run("HandIn, what it takes and what it gives with each roll", func(t *testing.T) {
		res, err := c.HandIn(ctx, &itemv1.HandInRequest{Actor: actor, QuestId: "sticky_samples", Completion: 2,
			Take: []*itemv1.HandInTake{{TemplateId: "slime_gel", Quantity: 5}},
			Give: []*itemv1.HandInGive{{TemplateId: "red_potion", Quantity: 10}, {TemplateId: "leather_cap", Quantity: 1, Stats: &itemv1.ItemStats{PhysicalDefense: 3}}}})
		if err != nil {
			t.Fatal(err)
		}
		in := f.handIn
		if in.Quest != "sticky_samples" || in.Completion != 2 || len(in.Take) != 1 || in.Take[0] != (model.Grant{Item: "slime_gel", Quantity: 5}) || f.actor.Character != character {
			t.Fatalf("hand-in %+v", in)
		}
		if len(in.Give) != 2 || in.Give[0] != (application.Give{TemplateID: "red_potion", Quantity: 10}) || in.Give[1].Stats == nil || *in.Give[1].Stats != (progression.ItemStats{PhysicalDefense: 3}) {
			t.Fatalf("gives %+v", in.Give)
		}
		wantChanged(t, res)
	})
	t.Run("HandedIn, each completion asked and each found with its reply", func(t *testing.T) {
		f.handedIn = []application.HandIn{{Quest: "sticky_samples", Completion: 2, Result: changed}}
		defer func() { f.handedIn = nil }()
		res, err := c.HandedIn(ctx, &itemv1.HandedInRequest{CharacterId: character.String(), Completions: []*itemv1.QuestCompletion{{QuestId: "sticky_samples", Completion: 2}, {QuestId: "slime_cleanup", Completion: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		if f.owner != character || !slices.Equal(f.asked, []application.Completion{{Quest: "sticky_samples", Completion: 2}, {Quest: "slime_cleanup", Completion: 1}}) {
			t.Fatalf("owner %v asked %+v", f.owner, f.asked)
		}
		got := res.GetHandedIn()
		if len(got) != 1 || got[0].GetQuestId() != "sticky_samples" || got[0].GetCompletion() != 2 {
			t.Fatalf("handed in %+v", got)
		}
		wantChanged(t, got[0])
	})
	t.Run("GrantStarter", func(t *testing.T) {
		res, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "bowman"})
		if err != nil {
			t.Fatal(err)
		}
		if f.class != kernel.Bowman || f.actor.Character != character {
			t.Fatalf("class %v actor %+v", f.class, f.actor)
		}
		wantChanged(t, res)
	})
}

func TestMalformedRequestsNeverReachTheModule(t *testing.T) {
	ctx := context.Background()
	f := &fakeItems{}
	c := client(t, f)
	bad := &itemv1.Actor{AccountId: "nope", CharacterId: character.String()}
	tests := []struct {
		name string
		call func() error
		want itemv1.Reason
	}{
		{"an inventory owner", func() error {
			_, err := c.Inventory(ctx, &itemv1.InventoryRequest{CharacterId: "x"})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an actor's account", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: bad, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an actor's character", func() error {
			_, err := c.Move(ctx, &itemv1.MoveRequest{Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: ""}, ItemId: potion.ID.String()})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an item", func() error {
			_, err := c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, ItemId: "bad", Quantity: 1})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"a cause", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: "nope"}})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"a cause that is not a UUIDv7", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: "6f1c2a3b-4d5e-4f60-8a71-92b3c4d5e6f7"}})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an empty idempotency key", func() error {
			_, err := c.Move(ctx, &itemv1.MoveRequest{Actor: actor, ItemId: potion.ID.String(), Tab: "use", Slot: 1})
			return err
		}, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
		{"an idempotency key that is no UUID", func() error {
			_, err := c.Discard(ctx, &itemv1.DiscardRequest{Actor: actor, IdempotencyKey: "discard-1", ItemId: potion.ID.String(), Quantity: 1})
			return err
		}, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
		{"an idempotency key of 4 KiB", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1 + strings.Repeat("x", 4096), TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP}})
			return err
		}, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
		{"a cause with no type", func() error {
			_, err := c.Add(ctx, &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{}})
			return err
		}, itemv1.Reason_REASON_BAD_CAUSE},
		{"a class", func() error {
			_, err := c.GrantStarter(ctx, &itemv1.GrantStarterRequest{Actor: actor, Class: "paladin"})
			return err
		}, itemv1.Reason_REASON_UNKNOWN_CLASS},
		{"an item to equip", func() error {
			_, err := c.Equip(ctx, &itemv1.EquipRequest{Actor: actor, IdempotencyKey: key1, ItemId: "bad", Level: 1, Class: "warrior"})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"the class an equip is checked against", func() error {
			_, err := c.Equip(ctx, &itemv1.EquipRequest{Actor: actor, IdempotencyKey: key1, ItemId: potion.ID.String(), Level: 1, Class: "paladin"})
			return err
		}, itemv1.Reason_REASON_UNKNOWN_CLASS},
		{"a hand-in's actor", func() error {
			_, err := c.HandIn(ctx, &itemv1.HandInRequest{Actor: bad, QuestId: "slime_cleanup", Completion: 1})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"the character a HandedIn asks about", func() error {
			_, err := c.HandedIn(ctx, &itemv1.HandedInRequest{CharacterId: "bad"})
			return err
		}, itemv1.Reason_REASON_BAD_ID},
		{"an unequip's idempotency key", func() error {
			_, err := c.Unequip(ctx, &itemv1.UnequipRequest{Actor: actor, IdempotencyKey: "nope", EquipSlot: "hat"})
			return err
		}, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(tt.want) || status.Code(err) != codes.InvalidArgument {
				t.Fatalf("got %v", err)
			}
		})
	}
	if f.calls != 0 {
		t.Fatalf("the module was called %d times", f.calls)
	}
}

// Each drop gets its own outcome in the request's order; a malformed one is refused alone, before the module runs the rest.
func TestAddManyAnswersEachDrop(t *testing.T) {
	key3 := "k-" + uuidv7.NewString()
	drop := func(key string, cause itemv1.CauseType) *itemv1.AddManyDrop {
		return &itemv1.AddManyDrop{IdempotencyKey: key, TemplateId: "slime_gel", Quantity: 2, Cause: &itemv1.Cause{Type: cause}}
	}
	f := &fakeItems{batch: application.Batch{
		Outcomes: []application.Outcome{{}, {Replayed: true, Result: changed}, {Err: fmt.Errorf("add: %w", model.ErrEtcTabFull)}},
		Changed:  []model.Slot{{Tab: gamedata.Etc, Slot: 1}},
		Revision: 13,
	}}
	res, err := client(t, f).AddMany(context.Background(), &itemv1.AddManyRequest{Actor: actor, Drops: []*itemv1.AddManyDrop{
		drop(key1, itemv1.CauseType_CAUSE_TYPE_DROP),
		drop("not-a-uuid", itemv1.CauseType_CAUSE_TYPE_DROP),
		drop(key2, itemv1.CauseType_CAUSE_TYPE_DROP),
		drop(key3, itemv1.CauseType_CAUSE_TYPE_UNSPECIFIED),
		drop(key3, itemv1.CauseType_CAUSE_TYPE_DROP),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.addMany) != 3 || f.addMany[0].Key != key1 || f.addMany[1].Key != key2 || f.addMany[2].Key != key3 || f.addMany[0].TemplateID != "slime_gel" || f.addMany[0].Quantity != 2 || f.addMany[0].Cause.Type != model.CauseDrop || f.actor.Character != character {
		t.Fatalf("the module got %+v for %+v", f.addMany, f.actor)
	}
	out := res.GetOutcomes()
	grants := []itemv1.Grant{itemv1.Grant_GRANT_GRANTED, itemv1.Grant_GRANT_REFUSED, itemv1.Grant_GRANT_ALREADY_GRANTED, itemv1.Grant_GRANT_REFUSED, itemv1.Grant_GRANT_REFUSED}
	reasons := []itemv1.Reason{0, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY, 0, itemv1.Reason_REASON_BAD_CAUSE, itemv1.Reason_REASON_ETC_TAB_FULL}
	for i := range grants {
		if out[i].GetGrant() != grants[i] || out[i].GetReason() != reasons[i] {
			t.Fatalf("drop %d: %+v, want %v %v", i, out[i], grants[i], reasons[i])
		}
	}
	wantChanged(t, out[2]) // the replay's stored reply
	if res.GetRevision() != 13 || len(res.GetChanged()) != 1 || res.GetChanged()[0].GetTab() != "etc" || res.GetChanged()[0].GetSlot() != 1 {
		t.Fatalf("the batch changed %+v at %d", res.GetChanged(), res.GetRevision())
	}
}

func TestAddManyRefusesABatchOfNoDropsOrTooMany(t *testing.T) {
	for _, n := range []int{0, application.MaxBatch + 1} {
		f := &fakeItems{}
		drops := make([]*itemv1.AddManyDrop, n)
		for i := range drops {
			drops[i] = &itemv1.AddManyDrop{IdempotencyKey: "k-" + uuidv7.NewString(), TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP}}
		}
		_, err := client(t, f).AddMany(context.Background(), &itemv1.AddManyRequest{Actor: actor, Drops: drops})
		if r, _ := grpcx.ReasonOf(err); status.Code(err) != codes.InvalidArgument || r != grpcx.Key(itemv1.Reason_REASON_BAD_BATCH_SIZE) || f.calls != 0 {
			t.Fatalf("%d drops: %v after %d calls", n, err, f.calls)
		}
	}
}

func TestEveryModuleErrorArrivesWithItsReason(t *testing.T) {
	for _, row := range handler.Errors {
		t.Run(row.Err.Error(), func(t *testing.T) {
			c := client(t, &fakeItems{err: row.Err})
			_, err := c.Move(context.Background(), &itemv1.MoveRequest{Actor: actor, IdempotencyKey: key1, ItemId: potion.ID.String(), Tab: "use", Slot: 1})
			want := grpcx.Reason{}
			if row.Reason != nil {
				want = grpcx.Key(row.Reason)
			}
			if r, ok := grpcx.ReasonOf(err); ok != (row.Reason != nil) || r != want || status.Code(err) != row.Code {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// A store that timed out answers Unavailable with no reason: a caller retries what carries no rule.
func TestATimeoutAsksTheCallerToRetry(t *testing.T) {
	c := client(t, &fakeItems{err: fmt.Errorf("add: %w", transient.ErrUnavailable)})
	_, err := c.Add(context.Background(), &itemv1.AddRequest{Actor: actor, IdempotencyKey: key1, TemplateId: "slime_gel", Quantity: 1, Cause: &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP}})
	if status.Code(err) != codes.Unavailable || grpcx.IsRule(err) {
		t.Fatalf("got %v, want Unavailable without a reason", err)
	}
}

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentineltest.Declared(filepath.Join("..", "model"))
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) == 0 {
		t.Fatal("no errors found; is the walk broken?")
	}
	mapped, err := sentineltest.Referenced("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in Errors", name)
		}
	}
	used := map[grpcx.Reason]bool{}
	for _, row := range handler.Errors {
		if row.Reason != nil {
			used[grpcx.Key(row.Reason)] = true
		}
	}
	for _, r := range grpctest.Reasons(itemv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
