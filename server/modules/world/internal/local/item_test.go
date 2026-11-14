package local_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/item"
	"finga/modules/world/internal/local"
	"finga/modules/world/internal/model"
	"finga/pkg/copytest"
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
)

// itemFake records what it is asked and answers canned values, or err.
type itemFake struct {
	actors []item.Actor

	adds     []item.AddRequest
	batches  [][]item.AddRequest
	uses     []item.UseRequest
	equips   []item.EquipRequest
	unequips []item.UnequipRequest
	handIns  []item.HandInRequest
	lookups  [][]item.Completion
	owners   []kernel.CharacterID

	batch     item.Batch
	result    item.Result
	replayed  bool
	inventory item.Inventory
	handedIn  func(asked []item.Completion) []item.HandedIn
	err       error
}

func (f *itemFake) Inventory(_ context.Context, owner kernel.CharacterID) (item.Inventory, error) {
	f.owners = append(f.owners, owner)
	return f.inventory, f.err
}

func (f *itemFake) Add(_ context.Context, a item.Actor, r item.AddRequest) (item.Result, error) {
	f.actors = append(f.actors, a)
	f.adds = append(f.adds, r)
	return f.result, f.err
}

func (f *itemFake) AddMany(_ context.Context, a item.Actor, drops []item.AddRequest) (item.Batch, error) {
	f.actors = append(f.actors, a)
	f.batches = append(f.batches, drops)
	return f.batch, f.err
}

func (f *itemFake) Use(_ context.Context, a item.Actor, r item.UseRequest) (item.Result, bool, error) {
	f.actors = append(f.actors, a)
	f.uses = append(f.uses, r)
	return f.result, f.replayed, f.err
}

func (f *itemFake) GrantStarter(_ context.Context, a item.Actor) (item.Result, error) {
	f.actors = append(f.actors, a)
	return item.Result{}, f.err
}

func (f *itemFake) Equip(_ context.Context, a item.Actor, r item.EquipRequest) (item.Result, error) {
	f.actors = append(f.actors, a)
	f.equips = append(f.equips, r)
	return f.result, f.err
}

func (f *itemFake) Unequip(_ context.Context, a item.Actor, r item.UnequipRequest) (item.Result, error) {
	f.actors = append(f.actors, a)
	f.unequips = append(f.unequips, r)
	return f.result, f.err
}

func (f *itemFake) HandIn(_ context.Context, a item.Actor, r item.HandInRequest) (item.Result, error) {
	f.actors = append(f.actors, a)
	f.handIns = append(f.handIns, r)
	return f.result, f.err
}

func (f *itemFake) HandedIn(_ context.Context, owner kernel.CharacterID, asked []item.Completion) ([]item.HandedIn, error) {
	f.owners = append(f.owners, owner)
	f.lookups = append(f.lookups, asked)
	if f.handedIn == nil {
		return nil, f.err
	}
	return f.handedIn(asked), f.err
}

func TestLootAsksForAnOwnersDropsEachUnderItsOwnKey(t *testing.T) {
	f := &itemFake{}
	roll := &progression.ItemStats{PhysicalAttack: 6, MaxHP: 10}
	in := model.GrantCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Drops: []model.Grant{
		{Key: uuidv7.New(), Template: "iron_sword", Quantity: 1, Stats: roll},
		{Key: uuidv7.New(), Template: "slime_gel", Quantity: 2},
	}}
	if _, err := local.NewLoot(f).AddMany(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if f.actors[0] != (item.Actor{Account: in.Account, Character: in.Character}) || len(f.batches) != 1 || len(f.batches[0]) != 2 {
		t.Fatalf("actors %+v, batches %+v", f.actors, f.batches)
	}
	for k, d := range f.batches[0] {
		g := in.Drops[k]
		want := item.AddRequest{Key: g.Key.String(), TemplateID: g.Template, Quantity: g.Quantity, Cause: item.Cause{Type: item.CauseDrop, ID: g.Key}, Stats: g.Stats}
		if !reflect.DeepEqual(d, want) {
			t.Fatalf("drop %d: %+v, want %+v", k, d, want)
		}
	}
}

// A roll crosses to the item module and back as a copy, as it did through the wire, so neither module sees the other change it.
func TestARollCrossesAsACopy(t *testing.T) {
	roll := &progression.ItemStats{PhysicalAttack: 6}
	f := &itemFake{result: item.Result{Changed: []item.Slot{{Item: &item.Item{Stats: &progression.ItemStats{PhysicalDefense: 3}}}}}}
	if _, err := local.NewLoot(f).AddMany(t.Context(), model.GrantCall{Drops: []model.Grant{{Key: uuidv7.New(), Stats: roll}}}); err != nil {
		t.Fatal(err)
	}
	got, err := local.NewEquipment(f).Give(t.Context(), model.GiftCall{Key: uuidv7.New(), Stats: roll})
	if err != nil {
		t.Fatal(err)
	}
	roll.PhysicalAttack, f.result.Changed[0].Item.Stats.PhysicalDefense = 99, 99
	if f.batches[0][0].Stats.PhysicalAttack != 6 || f.adds[0].Stats.PhysicalAttack != 6 || got.Slots[0].Item.Stats.PhysicalDefense != 3 {
		t.Fatalf("a roll is shared: sent %+v and %+v, got back %+v", f.batches[0][0].Stats, f.adds[0].Stats, got.Slots[0].Item.Stats)
	}
}

func TestLootReadsEachDropsAnswer(t *testing.T) {
	gel, potion := kernel.NewItemID(), kernel.NewItemID()
	rule := errors.New("the model's cause")
	f := &itemFake{batch: item.Batch{
		Outcomes: []item.Outcome{
			{},
			{Replayed: true, Result: item.Result{Revision: 8, Changed: []item.Slot{{Tab: gamedata.Use, Slot: 1, Item: &item.Item{ID: potion, TemplateID: "red_potion", Quantity: 1}}}}},
			{Err: fmt.Errorf("%w: %w", kernel.ErrEtcTabFull, rule)},
			{Err: kernel.ErrIdempotencyKeyReused},
			{Err: kernel.ErrUseTabFull},
			{Err: kernel.ErrEquipTabFull},
			{Err: kernel.ErrBadRoll},
			{Err: errors.New("not a rule")},
		},
		Changed:  []item.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &item.Item{ID: gel, TemplateID: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}},
		Revision: 31,
	}}
	got, err := local.NewLoot(f).AddMany(t.Context(), model.GrantCall{Drops: make([]model.Grant, 8)})
	if err != nil {
		t.Fatal(err)
	}
	badRoll, _ := kernel.ReasonOf(kernel.ErrBadRoll)
	want := model.Granted{
		Outcomes: []model.GrantOutcome{
			{Kind: model.GrantAdded},
			{Kind: model.GrantReplayed, Replayed: model.Inventory{Revision: 8, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 1, Item: &model.SlotItem{ID: potion, Template: "red_potion", Quantity: 1}}}}},
			{Kind: model.GrantNoRoom},
			{Kind: model.GrantReplayed},
			{Kind: model.GrantNoRoom},
			{Kind: model.GrantNoRoom},
			{Kind: model.GrantRefused, Reason: badRoll},
			{Kind: model.GrantRefused},
		},
		Added: model.Inventory{Revision: 31, Slots: []model.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestLootReturnsAFailedCallsError(t *testing.T) {
	cause := errors.New("down")
	if _, err := local.NewLoot(&itemFake{err: cause}).AddMany(t.Context(), model.GrantCall{}); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
}

func TestEquipmentWearsWithTheLiveLevelAndClass(t *testing.T) {
	sword, hat := kernel.NewItemID(), kernel.NewItemID()
	swordRoll, hatRoll := progression.ItemStats{PhysicalAttack: 17, MaxHP: 5}, progression.ItemStats{PhysicalDefense: 3, MaxHP: 10}
	wornSword := item.Item{ID: sword, TemplateID: "iron_sword", Quantity: 1, EquipSlot: "weapon", Stats: &swordRoll}
	wornHat := item.Item{ID: hat, TemplateID: "leather_cap", Quantity: 1, EquipSlot: "hat", Stats: &hatRoll}
	f := &itemFake{result: item.Result{
		Changed:   []item.Slot{{Tab: gamedata.Equip, Slot: 4}},
		Equipment: []item.EquipSlot{{Slot: "weapon", Item: &wornSword}, {Slot: "hat"}},
		Worn:      []item.Item{wornSword, wornHat, {ID: kernel.NewItemID(), TemplateID: "slime_gel"}, {TemplateID: "plain", EquipSlot: "ring"}},
		Revision:  21,
	}}
	e := local.NewEquipment(f)
	call := model.EquipCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Key: "k-1", Item: sword, EquipSlot: "ring1", Level: 9, Class: kernel.Class("warrior"), Stats: gamedata.Stats{STR: 1, DEX: 2}}
	got, err := e.Equip(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	wantReq := item.EquipRequest{Key: "k-1", ItemID: sword, EquipSlot: "ring1", Level: 9, Class: kernel.Class("warrior"), Stats: call.Stats}
	if f.actors[0] != (item.Actor{Account: call.Account, Character: call.Character}) || !reflect.DeepEqual(f.equips[0], wantReq) {
		t.Fatalf("sent %+v for %+v", f.equips[0], f.actors[0])
	}
	want := model.Equipped{
		Slots:     []model.Slot{{Tab: gamedata.Equip, Slot: 4}},
		Equipment: []model.EquipSlot{{Slot: "weapon", Item: &model.SlotItem{ID: sword, Template: "iron_sword", Quantity: 1, Stats: &swordRoll}}, {Slot: "hat"}},
		Worn:      model.Worn{Revision: 21, Items: []model.WornItem{{Template: "iron_sword", Stats: swordRoll}, {Template: "leather_cap", Stats: hatRoll}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestEquipmentUnequipsToASlotOrTheFirstFree(t *testing.T) {
	f := &itemFake{result: item.Result{Revision: 3}}
	e := local.NewEquipment(f)
	to := 6
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	for _, c := range []model.UnequipCall{
		{Account: account, Character: id, Key: "k-2", EquipSlot: "hat", ToSlot: &to},
		{Account: account, Character: id, Key: "k-3", EquipSlot: "hat"},
	} {
		got, err := e.Unequip(t.Context(), c)
		if err != nil || got.Worn.Revision != 3 {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
	want := []item.UnequipRequest{{Key: "k-2", EquipSlot: "hat", ToSlot: &to}, {Key: "k-3", EquipSlot: "hat"}}
	if !reflect.DeepEqual(f.unequips, want) || f.actors[1] != (item.Actor{Account: account, Character: id}) {
		t.Fatalf("sent %+v", f.unequips)
	}
}

func TestEquipmentUsesAnItemAndSaysWhetherItReplayed(t *testing.T) {
	potion := kernel.NewItemID()
	f := &itemFake{replayed: true, result: item.Result{Revision: 5, Changed: []item.Slot{{Tab: gamedata.Use, Slot: 1, Item: &item.Item{ID: potion, TemplateID: "red_potion", Quantity: 2}}}}}
	c := model.UseCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Key: "k-4", Item: potion, Template: "red_potion"}
	got, replayed, err := local.NewEquipment(f).Use(t.Context(), c)
	if err != nil || !replayed {
		t.Fatalf("replayed %v, %v", replayed, err)
	}
	want := model.Inventory{Revision: 5, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 1, Item: &model.SlotItem{ID: potion, Template: "red_potion", Quantity: 2}}}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(f.uses, []item.UseRequest{{Key: "k-4", ItemID: potion, Template: "red_potion"}}) || f.actors[0] != (item.Actor{Account: c.Account, Character: c.Character}) {
		t.Fatalf("got %+v, sent %+v", got, f.uses)
	}
}

func TestHandInSendsTheQuestsLinesAndReadsWhatChanged(t *testing.T) {
	gel := kernel.NewItemID()
	f := &itemFake{result: item.Result{Revision: 9, Changed: []item.Slot{{Tab: gamedata.Etc, Slot: 0}, {Tab: gamedata.Etc, Slot: 1, Item: &item.Item{ID: gel, TemplateID: "slime_gel", Quantity: 3}}}}}
	roll := &progression.ItemStats{PhysicalDefense: 3}
	call := model.HandInCall{
		Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Quest: "bounty", Completion: 2,
		Take: []gamedata.Grant{{Item: "slime_gel", Quantity: 2}},
		Give: []model.Reward{{Template: "leather_cap", Quantity: 1, Stats: roll}, {Template: "red_potion", Quantity: 3}},
	}
	got, err := local.NewEquipment(f).HandIn(t.Context(), call)
	if err != nil {
		t.Fatal(err)
	}
	want := item.HandInRequest{
		Quest: "bounty", Completion: 2, Take: call.Take,
		Give: []item.Give{{TemplateID: "leather_cap", Quantity: 1, Stats: roll}, {TemplateID: "red_potion", Quantity: 3}},
	}
	if len(f.handIns) != 1 || !reflect.DeepEqual(f.handIns[0], want) || f.actors[0] != (item.Actor{Account: call.Account, Character: call.Character}) {
		t.Fatalf("sent %+v", f.handIns)
	}
	if got.Revision != 9 || len(got.Slots) != 2 || got.Slots[0].Item != nil || got.Slots[1].Item.ID != gel || got.Slots[1].Item.Quantity != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestGiveAddsOneItemUnderTheGiftsKey(t *testing.T) {
	f := &itemFake{result: item.Result{Revision: 4}}
	roll := &progression.ItemStats{PhysicalAttack: 2}
	c := model.GiftCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Key: uuidv7.New(), Template: "wooden_sword", Stats: roll}
	got, err := local.NewEquipment(f).Give(t.Context(), c)
	if err != nil || got.Revision != 4 {
		t.Fatalf("got %+v, %v", got, err)
	}
	want := item.AddRequest{Key: c.Key.String(), TemplateID: "wooden_sword", Quantity: 1, Cause: item.Cause{Type: item.CausePromotion, ID: c.Key}, Stats: roll}
	if !reflect.DeepEqual(f.adds, []item.AddRequest{want}) || f.actors[0] != (item.Actor{Account: c.Account, Character: c.Character}) {
		t.Fatalf("sent %+v", f.adds)
	}
}

func TestHandedInAsksEachCompletionAndReadsWhatChanged(t *testing.T) {
	gel := kernel.NewItemID()
	f := &itemFake{handedIn: func(asked []item.Completion) []item.HandedIn {
		return []item.HandedIn{{Quest: asked[0].Quest, Completion: asked[0].Completion, Result: item.Result{Revision: 9, Changed: []item.Slot{{Tab: gamedata.Etc, Slot: 1, Item: &item.Item{ID: gel, TemplateID: "slime_gel", Quantity: 3}}}}}}
	}}
	id := kernel.NewCharacterID()
	got, err := local.NewEquipment(f).HandedIn(t.Context(), id, []model.Completion{{Quest: "bounty", Completion: 2}, {Quest: "errand", Completion: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.lookups, [][]item.Completion{{{Quest: "bounty", Completion: 2}, {Quest: "errand", Completion: 1}}}) || f.owners[0] != id {
		t.Fatalf("asked %+v of %v", f.lookups, f.owners)
	}
	want := []model.HandedIn{{Quest: "bounty", Completion: 2, Items: model.Inventory{Revision: 9, Slots: []model.Slot{{Tab: gamedata.Etc, Slot: 1, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 3}}}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestHandedInAsksInBatchesOfTheItemModulesLimit(t *testing.T) {
	f := &itemFake{handedIn: func(asked []item.Completion) []item.HandedIn {
		return []item.HandedIn{{Quest: asked[0].Quest, Completion: asked[0].Completion}}
	}}
	asked := make([]model.Completion, 33)
	for k := range asked {
		asked[k] = model.Completion{Quest: fmt.Sprintf("q%02d", k), Completion: 1}
	}
	got, err := local.NewEquipment(f).HandedIn(t.Context(), kernel.NewCharacterID(), asked)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.lookups) != 2 || len(f.lookups[0]) != 32 || len(f.lookups[1]) != 1 {
		t.Fatalf("asked %d times; want 32 completions, then 1", len(f.lookups))
	}
	if len(got) != 2 || got[0].Quest != "q00" || got[1].Quest != "q32" {
		t.Fatalf("got %+v; want each batch's answer, in order", got)
	}
}

func TestHandedInAsksNothingOfNoCompletions(t *testing.T) {
	f := &itemFake{}
	got, err := local.NewEquipment(f).HandedIn(t.Context(), kernel.NewCharacterID(), nil)
	if err != nil || len(got) != 0 || len(f.lookups) != 0 {
		t.Fatalf("got %+v, %v after %d calls", got, err, len(f.lookups))
	}
}

func TestWornReadsOnlyWhatIsWornWithARoll(t *testing.T) {
	roll := progression.ItemStats{PhysicalAttack: 17}
	f := &itemFake{inventory: item.Inventory{Revision: 12, Items: []item.Item{
		{TemplateID: "iron_sword", EquipSlot: "weapon", Stats: &roll},
		{TemplateID: "spare_sword", Stats: &roll},
		{TemplateID: "slime_gel", EquipSlot: "weapon"},
	}}}
	id := kernel.NewCharacterID()
	got, err := local.NewEquipment(f).Worn(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if want := (model.Worn{Revision: 12, Items: []model.WornItem{{Template: "iron_sword", Stats: roll}}}); !reflect.DeepEqual(got, want) || f.owners[0] != id {
		t.Fatalf("got %+v", got)
	}
}

func TestEquipmentFailuresKeepARuleAndOtherwiseAreTransient(t *testing.T) {
	rule := fmt.Errorf("%w: level 9", kernel.ErrLevelTooLow)
	ctx := t.Context()
	id := kernel.NewCharacterID()
	calls := map[string]func(e local.Equipment) error{
		"Equip":   func(e local.Equipment) error { _, err := e.Equip(ctx, model.EquipCall{}); return err },
		"Unequip": func(e local.Equipment) error { _, err := e.Unequip(ctx, model.UnequipCall{}); return err },
		"Use":     func(e local.Equipment) error { _, _, err := e.Use(ctx, model.UseCall{}); return err },
		"HandIn":  func(e local.Equipment) error { _, err := e.HandIn(ctx, model.HandInCall{}); return err },
		"Give":    func(e local.Equipment) error { _, err := e.Give(ctx, model.GiftCall{}); return err },
		"HandedIn": func(e local.Equipment) error {
			_, err := e.HandedIn(ctx, id, []model.Completion{{Quest: "q"}})
			return err
		},
		"Worn": func(e local.Equipment) error { _, err := e.Worn(ctx, id); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(local.NewEquipment(&itemFake{err: rule})); !errors.Is(err, rule) || errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("a rule arrived as %v", err)
			}
			cause := errors.New("database restarting")
			err := call(local.NewEquipment(&itemFake{err: cause}))
			if !errors.Is(err, transient.ErrUnavailable) || !errors.Is(err, cause) || kernel.IsRule(err) {
				t.Fatalf("a failure arrived as %v; want it transient, with its cause", err)
			}
		})
	}
}

func TestLootRetriesAnythingButARule(t *testing.T) {
	l := local.NewLoot(nil)
	if l.Retryable(kernel.ErrEquipTabFull) || !l.Retryable(kernel.ErrInternal) || !l.Retryable(errors.New("down")) {
		t.Fatal("only a failure that is no rule is retried")
	}
}

func TestStarterAsksForTheCharacterOnJoin(t *testing.T) {
	f := &itemFake{}
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	if err := local.NewStarter(f).GrantStarter(t.Context(), account, id); err != nil {
		t.Fatal(err)
	}
	if len(f.actors) != 1 || f.actors[0] != (item.Actor{Account: account, Character: id}) {
		t.Fatalf("actors %+v", f.actors)
	}
	cause := errors.New("down")
	if err := local.NewStarter(&itemFake{err: cause}).GrantStarter(t.Context(), account, id); !errors.Is(err, cause) {
		t.Fatalf("got %v", err)
	}
}

func last[T any](s []T) T { return s[len(s)-1] }

// sent is what an adapter hands the item module: the actor and the request.
type sent[Req any] struct {
	Actor item.Actor
	Req   Req
}

// callRenames are a call's fields where the item module takes them: the account and character as its actor, the others in the request under the names given, "" for the request itself.
func callRenames(fields map[string]string) map[string]string {
	out := map[string]string{"Account": "Actor.Account", "Character": "Actor.Character"}
	for from, to := range fields {
		out[from] = strings.TrimSuffix("Req."+to, ".")
	}
	return out
}

// Each call reaches the item module with every field, the account and character as its actor.
func TestEachItemCallKeepsItsFields(t *testing.T) {
	f := &itemFake{}
	e := local.NewEquipment(f)
	for name, diff := range map[string]func() []string{
		"Equip": func() []string {
			m := copytest.Match{Renames: callRenames(map[string]string{"Key": "Key", "Item": "ItemID", "EquipSlot": "EquipSlot", "Level": "Level", "Class": "Class", "Stats": "Stats"})}
			return copytest.Diff(m, func(c model.EquipCall) sent[item.EquipRequest] {
				_, _ = e.Equip(t.Context(), c)
				return sent[item.EquipRequest]{last(f.actors), last(f.equips)}
			})
		},
		"Unequip": func() []string {
			m := copytest.Match{Renames: callRenames(map[string]string{"Key": "Key", "EquipSlot": "EquipSlot", "ToSlot": "ToSlot"})}
			return copytest.Diff(m, func(c model.UnequipCall) sent[item.UnequipRequest] {
				_, _ = e.Unequip(t.Context(), c)
				return sent[item.UnequipRequest]{last(f.actors), last(f.unequips)}
			})
		},
		"Use": func() []string {
			m := copytest.Match{Renames: callRenames(map[string]string{"Key": "Key", "Item": "ItemID", "Template": "Template"})}
			return copytest.Diff(m, func(c model.UseCall) sent[item.UseRequest] {
				_, _, _ = e.Use(t.Context(), c)
				return sent[item.UseRequest]{last(f.actors), last(f.uses)}
			})
		},
		"HandIn": func() []string {
			m := copytest.Match{Renames: callRenames(map[string]string{"Quest": "Quest", "Completion": "Completion", "Take": "Take", "Give": "Give", "Give.Template": "Give.TemplateID"})}
			return copytest.Diff(m, func(c model.HandInCall) sent[item.HandInRequest] {
				_, _ = e.HandIn(t.Context(), c)
				return sent[item.HandInRequest]{last(f.actors), last(f.handIns)}
			})
		},
		"Give": func() []string {
			m := copytest.Match{
				Renames: callRenames(map[string]string{"Key": "Key", "Template": "TemplateID", "Stats": "Stats"}),
				Computed: map[string]string{
					"Req.Quantity":   "a gift is one item",
					"Req.Cause.Type": "a trainer's gift, always",
					"Req.Cause.ID":   "the gift's key, as TestGiveAddsOneItemUnderTheGiftsKey checks",
				},
			}
			return copytest.Diff(m, func(c model.GiftCall) sent[item.AddRequest] {
				_, _ = e.Give(t.Context(), c)
				return sent[item.AddRequest]{last(f.actors), last(f.adds)}
			})
		},
		"AddMany": func() []string {
			m := copytest.Match{
				Renames: callRenames(map[string]string{"Drops": "", "Drops.Template": "TemplateID"}),
				Computed: map[string]string{
					"Req.Cause.Type": "a drop's, always",
					"Req.Cause.ID":   "the drop's key, as TestLootAsksForAnOwnersDropsEachUnderItsOwnKey checks",
				},
			}
			return copytest.Diff(m, func(c model.GrantCall) sent[[]item.AddRequest] {
				_, _ = local.NewLoot(f).AddMany(t.Context(), c)
				return sent[[]item.AddRequest]{last(f.actors), last(f.batches)}
			})
		},
		"HandedIn": func() []string {
			type asked struct {
				Owner kernel.CharacterID
				Asked []model.Completion
			}
			return copytest.Diff(copytest.Match{}, func(a asked) any {
				_, _ = e.HandedIn(t.Context(), a.Owner, a.Asked)
				return struct {
					Owner kernel.CharacterID
					Asked []item.Completion
				}{last(f.owners), last(f.lookups)}
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// slotsMatch is a match with each item result's changed slots at from as the world's at to: each item under its template, where it sits named by its slot.
func slotsMatch(m copytest.Match, from, to string) copytest.Match {
	if m.Renames == nil {
		m.Renames, m.Skip = map[string]string{}, map[string]string{}
	}
	m.Renames[from+".Changed"] = to + ".Slots"
	m.Renames[from+".Changed.Item.TemplateID"] = to + ".Slots.Item.Template"
	m.Renames[from+".Revision"] = to + ".Revision"
	m.Skip[from+".Changed.Item.Tab"] = "the slot names the item's tab"
	m.Skip[from+".Changed.Item.Slot"] = "the slot names it"
	m.Skip[from+".Changed.Item.EquipSlot"] = "an item in a tab is worn nowhere"
	return m
}

// unworn is m with a result's equip slots and worn items skipped, for a call that changes neither.
func unworn(m copytest.Match, from string) copytest.Match {
	m.Skip[from+".Equipment"] = "only an equip, an unequip or a starter grant changes an equip slot"
	m.Skip[from+".Worn"] = "only an equip or an unequip lists what is worn"
	return m
}

// result is an item result under a name, so a match can name its root.
type result struct{ R item.Result }

// Each answer of the item module reaches the world with every field: each changed slot's item with its roll, and after an equip or unequip what is worn.
func TestEachItemAnswerKeepsItsFields(t *testing.T) {
	f := &itemFake{}
	e := local.NewEquipment(f)
	inventory := unworn(slotsMatch(copytest.Match{}, "R", ""), "R")
	equipped := slotsMatch(copytest.Match{}, "R", "")
	equipped.Renames["R.Revision"] = "Worn.Revision"
	equipped.Renames["R.Equipment"] = "Equipment"
	equipped.Renames["R.Equipment.Item.TemplateID"] = "Equipment.Item.Template"
	equipped.Renames["R.Worn"] = "Worn.Items"
	equipped.Renames["R.Worn.TemplateID"] = "Worn.Items.Template"
	for _, field := range []string{"Tab", "Slot", "EquipSlot"} {
		equipped.Skip["R.Equipment.Item."+field] = "a worn item is in no tab, and its equip slot names where it is worn"
	}
	for _, field := range []string{"ID", "Tab", "Slot", "Quantity", "EquipSlot"} {
		equipped.Skip["R.Worn."+field] = "the stats count what is worn by its template and roll"
	}
	for name, diff := range map[string]func() []string{
		"Equip": func() []string {
			return copytest.Diff(equipped, func(r result) model.Equipped {
				f.result = r.R
				out, _ := e.Equip(t.Context(), model.EquipCall{})
				return out
			})
		},
		"Unequip": func() []string {
			return copytest.Diff(equipped, func(r result) model.Equipped {
				f.result = r.R
				out, _ := e.Unequip(t.Context(), model.UnequipCall{})
				return out
			})
		},
		"Use": func() []string {
			return copytest.Diff(inventory, func(r result) model.Inventory {
				f.result = r.R
				out, _, _ := e.Use(t.Context(), model.UseCall{})
				return out
			})
		},
		"HandIn": func() []string {
			return copytest.Diff(inventory, func(r result) model.Inventory {
				f.result = r.R
				out, _ := e.HandIn(t.Context(), model.HandInCall{})
				return out
			})
		},
		"Give": func() []string {
			return copytest.Diff(inventory, func(r result) model.Inventory {
				f.result = r.R
				out, _ := e.Give(t.Context(), model.GiftCall{})
				return out
			})
		},
		"HandedIn": func() []string {
			m := unworn(slotsMatch(copytest.Match{}, "Result", "Items"), "Result")
			return copytest.Diff(m, func(handed []item.HandedIn) []model.HandedIn {
				f.handedIn = func([]item.Completion) []item.HandedIn { return handed }
				out, _ := e.HandedIn(t.Context(), kernel.CharacterID{}, []model.Completion{{}})
				return out
			})
		},
		"Worn": func() []string {
			m := copytest.Match{Renames: map[string]string{"Items.TemplateID": "Items.Template"}, Skip: map[string]string{}}
			for _, field := range []string{"ID", "Tab", "Slot", "Quantity", "EquipSlot"} {
				m.Skip["Items."+field] = "the stats count what is worn by its template and roll"
			}
			return copytest.Diff(m, func(inv item.Inventory) model.Worn {
				f.inventory = inv
				out, _ := e.Worn(t.Context(), kernel.CharacterID{})
				return out
			})
		},
		"AddMany": func() []string {
			type batch struct {
				Changed  []item.Slot
				Revision uint64
				Replayed result // a replayed drop's stored reply
			}
			m := slotsMatch(copytest.Match{}, "Replayed.R", "Outcomes.Replayed")
			m = unworn(m, "Replayed.R")
			m.Renames["Changed"] = "Added.Slots"
			m.Renames["Changed.Item.TemplateID"] = "Added.Slots.Item.Template"
			m.Renames["Revision"] = "Added.Revision"
			m.Skip["Changed.Item.Tab"] = "the slot names the item's tab"
			m.Skip["Changed.Item.Slot"] = "the slot names it"
			m.Skip["Changed.Item.EquipSlot"] = "an item in a tab is worn nowhere"
			m.Computed = map[string]string{"Outcomes.Reason": "a refused drop's rule, which TestLootReadsEachDropsAnswer reads"}
			m.Enums = map[reflect.Type]any{reflect.TypeFor[model.GrantKind](): model.GrantReplayed}
			return copytest.Diff(m, func(b batch) model.Granted {
				f.batch = item.Batch{Outcomes: []item.Outcome{{Replayed: true, Result: b.Replayed.R}}, Changed: b.Changed, Revision: b.Revision}
				out, _ := local.NewLoot(f).AddMany(t.Context(), model.GrantCall{})
				return out
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
