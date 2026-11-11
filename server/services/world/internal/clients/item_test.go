package clients_test

import (
	"context"
	"errors"
	"reflect"
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
	"finga/pkg/transient"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/model"
)

type items struct {
	itemv1.UnimplementedItemServiceServer
	mu       sync.Mutex
	batches  []*itemv1.AddManyRequest
	answer   *itemv1.AddManyResponse // every AddMany's
	grants   []*itemv1.GrantStarterRequest
	equips   []*itemv1.EquipRequest
	unequips []*itemv1.UnequipRequest
	handIns  []*itemv1.HandInRequest
	changed  []*itemv1.Slot
	worn     []*itemv1.EquipSlot // the equip slots an equip or unequip changed
	held     []*itemv1.Item
	revision uint64   // every answer's
	asked    []string // each Inventory's character
	err      error
}

func (f *items) Inventory(_ context.Context, req *itemv1.InventoryRequest) (*itemv1.InventoryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, req.GetCharacterId())
	return &itemv1.InventoryResponse{Items: f.held, Revision: f.revision}, f.err
}

func (f *items) AddMany(_ context.Context, req *itemv1.AddManyRequest) (*itemv1.AddManyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, req)
	return f.answer, f.err
}

func (f *items) GrantStarter(_ context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, req)
	return &itemv1.GrantStarterResponse{}, f.err
}

func (f *items) Equip(_ context.Context, req *itemv1.EquipRequest) (*itemv1.EquipResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.equips = append(f.equips, req)
	return &itemv1.EquipResponse{Changed: f.changed, Equipment: f.worn, Worn: f.held, Revision: f.revision}, f.err
}

func (f *items) Unequip(_ context.Context, req *itemv1.UnequipRequest) (*itemv1.UnequipResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unequips = append(f.unequips, req)
	return &itemv1.UnequipResponse{Changed: f.changed, Equipment: f.worn, Worn: f.held, Revision: f.revision}, f.err
}

func (f *items) HandIn(_ context.Context, req *itemv1.HandInRequest) (*itemv1.HandInResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handIns = append(f.handIns, req)
	return &itemv1.HandInResponse{Changed: f.changed, Revision: f.revision}, f.err
}

func itemClient(t *testing.T, f *items) itemv1.ItemServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	itemv1.RegisterItemServiceServer(srv, f)
	return itemv1.NewItemServiceClient(grpctest.Conn(t, srv))
}

func TestLootAsksForAnOwnersDropsEachUnderItsOwnKey(t *testing.T) {
	f := &items{answer: &itemv1.AddManyResponse{Outcomes: []*itemv1.DropOutcome{{Grant: itemv1.Grant_GRANT_GRANTED}, {Grant: itemv1.Grant_GRANT_GRANTED}}}}
	in := model.GrantCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Drops: []model.Grant{
		{Key: uuidv7.New(), Template: "iron_sword", Quantity: 1, Stats: &progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}},
		{Key: uuidv7.New(), Template: "slime_gel", Quantity: 2},
	}}
	if _, err := clients.NewLoot(itemClient(t, f)).AddMany(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	req := f.batches[0]
	if req.GetActor().GetAccountId() != in.Account.String() || req.GetActor().GetCharacterId() != in.Character.String() || len(req.GetDrops()) != 2 {
		t.Fatalf("request %+v", req)
	}
	for k, d := range req.GetDrops() {
		key := in.Drops[k].Key.String()
		if d.GetIdempotencyKey() != key || d.GetCause().GetId() != key || d.GetCause().GetType() != itemv1.CauseType_CAUSE_TYPE_DROP {
			t.Fatalf("drop %d: its key is not both the idempotency key and the cause: %+v", k, d)
		}
		if d.GetTemplateId() != in.Drops[k].Template || int(d.GetQuantity()) != in.Drops[k].Quantity {
			t.Fatalf("drop %d: %+v", k, d)
		}
	}
	want := &itemv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}
	if s := req.GetDrops()[0].GetStats(); !proto.Equal(s, want) {
		t.Fatalf("the sword's roll sent %v, want %v", s, want)
	}
	if s := req.GetDrops()[1].GetStats(); s != nil {
		t.Fatalf("a gel sent the roll %+v", s)
	}
}

// Each drop's answer becomes its outcome: a reused key, under another fingerprint, means an earlier try granted it.
func TestLootReadsEachDropsAnswer(t *testing.T) {
	gel, potion := kernel.NewItemID(), kernel.NewItemID()
	f := &items{answer: &itemv1.AddManyResponse{
		Outcomes: []*itemv1.DropOutcome{
			{Grant: itemv1.Grant_GRANT_GRANTED},
			{Grant: itemv1.Grant_GRANT_ALREADY_GRANTED, Changed: []*itemv1.Slot{{Tab: "use", Slot: 1, Item: &itemv1.Item{Id: potion.String(), TemplateId: "red_potion", Quantity: 1}}}, Revision: 8},
			{Grant: itemv1.Grant_GRANT_REFUSED, Reason: itemv1.Reason_REASON_INVENTORY_FULL},
			{Grant: itemv1.Grant_GRANT_REFUSED, Reason: itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED},
			{Grant: itemv1.Grant_GRANT_REFUSED, Reason: itemv1.Reason_REASON_BAD_ROLL},
		},
		Changed:  []*itemv1.Slot{{Tab: "etc", Slot: 2, Item: &itemv1.Item{Id: gel.String(), TemplateId: "slime_gel", Quantity: 7}}, {Tab: "etc", Slot: 4}},
		Revision: 31,
	}}
	got, err := clients.NewLoot(itemClient(t, f)).AddMany(context.Background(), model.GrantCall{Drops: make([]model.Grant, 5)})
	if err != nil {
		t.Fatal(err)
	}
	want := model.Granted{
		Outcomes: []model.GrantOutcome{
			{Kind: model.GrantAdded},
			{Kind: model.GrantReplayed, Replayed: model.Inventory{Revision: 8, Slots: []model.Slot{{Tab: gamedata.Use, Slot: 1, Item: &model.SlotItem{ID: potion, Template: "red_potion", Quantity: 1}}}}},
			{Kind: model.GrantNoRoom},
			{Kind: model.GrantReplayed},
			{Kind: model.GrantRefused, Reason: "REASON_BAD_ROLL"},
		},
		Added: model.Inventory{Revision: 31, Slots: []model.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestLootFailsAnAnswerItCannotRead(t *testing.T) {
	bad := &itemv1.Item{Id: "nope", TemplateId: "slime_gel"}
	for name, answer := range map[string]*itemv1.AddManyResponse{
		"an outcome it does not know":           {Outcomes: []*itemv1.DropOutcome{{}}},
		"a malformed item ID among the changes": {Outcomes: []*itemv1.DropOutcome{{Grant: itemv1.Grant_GRANT_GRANTED}}, Changed: []*itemv1.Slot{{Item: bad}}},
		"a malformed item ID in a stored reply": {Outcomes: []*itemv1.DropOutcome{{Grant: itemv1.Grant_GRANT_ALREADY_GRANTED, Changed: []*itemv1.Slot{{Item: bad}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := clients.NewLoot(itemClient(t, &items{answer: answer})).AddMany(context.Background(), model.GrantCall{Drops: make([]model.Grant, 1)}); err == nil {
				t.Fatal("read")
			}
		})
	}
	down := status.Error(codes.Unavailable, "down")
	if _, err := clients.NewLoot(itemClient(t, &items{err: down})).AddMany(context.Background(), model.GrantCall{Drops: make([]model.Grant, 1)}); status.Code(err) != codes.Unavailable {
		t.Fatalf("an outage arrived as %v", err)
	}
}

func TestLootRetriesOnlyWhatMayPass(t *testing.T) {
	p := clients.NewLoot(nil)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a full inventory is a rule", grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), false},
		{"an unreachable service may pass", status.Error(codes.Unavailable, "connection refused"), true},
		{"an unexplained failure may pass", status.Error(codes.Internal, "internal error"), true},
		{"a malformed item ID in a reply", errors.New("bad item ID"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Retryable(tt.err); got != tt.want {
				t.Fatalf("Retryable = %v", got)
			}
		})
	}
}

func TestStarterAsksForTheClassOnJoin(t *testing.T) {
	f := &items{}
	s := clients.NewStarter(itemClient(t, f))
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	if err := s.GrantStarter(context.Background(), account, id, kernel.Warrior); err != nil {
		t.Fatal(err)
	}
	if g := f.grants[0]; g.GetClass() != "warrior" || g.GetActor().GetCharacterId() != id.String() || g.GetActor().GetAccountId() != account.String() {
		t.Fatalf("grant %+v", g)
	}
}

func TestEquipmentWearsWithTheLiveLevelAndClass(t *testing.T) {
	sword, hat := kernel.NewItemID(), kernel.NewItemID()
	wornSword := &itemv1.Item{Id: sword.String(), TemplateId: "iron_sword", Quantity: 1, EquipSlot: "weapon", Stats: &itemv1.ItemStats{PhysicalAttack: 17, MaxHp: 5}}
	wornHat := &itemv1.Item{Id: hat.String(), TemplateId: "leather_cap", Quantity: 1, EquipSlot: "hat", Stats: &itemv1.ItemStats{PhysicalDefense: 3, MaxHp: 10}}
	f := &items{
		changed: []*itemv1.Slot{{Tab: "equip", Slot: 4}}, worn: []*itemv1.EquipSlot{{EquipSlot: "weapon", Item: wornSword}},
		held: []*itemv1.Item{wornSword, wornHat}, revision: 21,
	}
	e := clients.NewEquipment(itemClient(t, f))
	call := model.EquipCall{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Key: "k-1", Item: sword, Level: 9, Class: kernel.Warrior, Stats: gamedata.Stats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5}}
	got, err := e.Equip(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	req := f.equips[0]
	if req.GetIdempotencyKey() != "k-1" || req.GetItemId() != sword.String() || req.GetLevel() != 9 || req.GetClass() != "warrior" || req.GetActor().GetCharacterId() != call.Character.String() || req.GetActor().GetAccountId() != call.Account.String() {
		t.Fatalf("request %+v", req)
	}
	if own := req.GetStats(); !proto.Equal(own, &itemv1.PrimaryStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5}) {
		t.Fatalf("own stats sent %v, want the character's", own)
	}
	want := model.Worn{Revision: 21, Items: []model.WornItem{{Template: "iron_sword", Stats: progression.ItemStats{PhysicalAttack: 17, MaxHP: 5}}, {Template: "leather_cap", Stats: progression.ItemStats{PhysicalDefense: 3, MaxHP: 10}}}}
	if !reflect.DeepEqual(got.Worn, want) {
		t.Fatalf("worn %+v; want each item worn, with its template and roll, at revision 21", got.Worn)
	}
	if len(got.Slots) != 1 || got.Slots[0].Slot != 4 || got.Slots[0].Item != nil || len(got.Equipment) != 1 || got.Equipment[0].Slot != "weapon" || got.Equipment[0].Item.ID != sword || *got.Equipment[0].Item.Stats != (progression.ItemStats{PhysicalAttack: 17, MaxHP: 5}) {
		t.Fatalf("changed %+v, equipment %+v", got.Slots, got.Equipment)
	}

	to := 6
	if _, err := e.Unequip(context.Background(), model.UnequipCall{Key: "k-2", EquipSlot: "hat", ToSlot: &to}); err != nil || f.unequips[0].GetToSlot() != 6 || f.unequips[0].GetEquipSlot() != "hat" {
		t.Fatalf("unequip %+v, %v", f.unequips, err)
	}
	if _, err := e.Unequip(context.Background(), model.UnequipCall{Key: "k-3", EquipSlot: "hat"}); err != nil || f.unequips[1].ToSlot != nil {
		t.Fatalf("an unequip to the first free slot sent %+v, %v", f.unequips[1], err)
	}

	w, err := e.Worn(context.Background(), call.Character)
	if err != nil || !reflect.DeepEqual(w, want) {
		t.Fatalf("worn %+v, %v", w, err)
	}
}

func TestAWornRollIsReadWhole(t *testing.T) {
	sword := kernel.NewItemID()
	worn := &itemv1.Item{Id: sword.String(), TemplateId: "iron_sword", Quantity: 1, EquipSlot: "weapon", Stats: &itemv1.ItemStats{Str: 1, Dex: 2, Int: 3, Agi: 4, Vit: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHp: 10, MaxMp: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HpRegen: 17, MpRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}}
	f := &items{worn: []*itemv1.EquipSlot{{EquipSlot: "weapon", Item: worn}}, held: []*itemv1.Item{worn}, revision: 2}
	got, err := clients.NewEquipment(itemClient(t, f)).Equip(context.Background(), model.EquipCall{Key: "k-1", Item: sword, Class: kernel.Warrior})
	if err != nil {
		t.Fatal(err)
	}
	want := progression.ItemStats{STR: 1, DEX: 2, INT: 3, AGI: 4, VIT: 5, PhysicalAttack: 6, MagicAttack: 7, PhysicalDefense: 8, MagicDefense: 9, MaxHP: 10, MaxMP: 11, Accuracy: 12, Evasion: 13, CritRate: 14, CritDamage: 15, Penetration: 16, HPRegen: 17, MPRegen: 18, AttackSpeed: 19, CooldownSpeed: 20, MoveSpeed: 21}
	if *got.Equipment[0].Item.Stats != want || len(got.Worn.Items) != 1 || got.Worn.Items[0] != (model.WornItem{Template: "iron_sword", Stats: want}) {
		t.Fatalf("equipment %+v, worn %+v; want every stat in its own field", *got.Equipment[0].Item.Stats, got.Worn.Items)
	}
}

func TestEquipmentFailures(t *testing.T) {
	refused := grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(itemv1.Reason_REASON_LEVEL_TOO_LOW))
	e := clients.NewEquipment(itemClient(t, &items{err: refused}))
	_, err := e.Equip(context.Background(), model.EquipCall{Key: "k-1", Item: kernel.NewItemID(), Class: kernel.Warrior})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(itemv1.Reason_REASON_LEVEL_TOO_LOW) {
		t.Fatalf("a refusal arrived as %v; want its reason kept", err)
	}
	e = clients.NewEquipment(itemClient(t, &items{err: status.Error(codes.Unavailable, "down")}))
	if _, err := e.Equip(context.Background(), model.EquipCall{Key: "k-1", Item: kernel.NewItemID(), Class: kernel.Warrior}); !errors.Is(err, transient.ErrUnavailable) {
		t.Fatalf("an outage arrived as %v; want it transient", err)
	}
	if _, err := e.Worn(context.Background(), kernel.NewCharacterID()); !errors.Is(err, transient.ErrUnavailable) {
		t.Fatalf("a read in an outage arrived as %v; want it transient", err)
	}
}

func TestHandInSendsTheQuestsLinesAndReadsWhatChanged(t *testing.T) {
	gel := kernel.NewItemID()
	f := &items{changed: []*itemv1.Slot{{Tab: "etc", Slot: 0}, {Tab: "etc", Slot: 1, Item: &itemv1.Item{Id: gel.String(), TemplateId: "slime_gel", Quantity: 3}}}, revision: 9}
	e := clients.NewEquipment(itemClient(t, f))
	call := model.HandInCall{
		Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), Quest: "bounty", Completion: 2,
		Take: []gamedata.Grant{{Item: "slime_gel", Quantity: 2}},
		Give: []model.Reward{{Template: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{PhysicalDefense: 3}}, {Template: "red_potion", Quantity: 3}},
	}
	got, err := e.HandIn(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	want := &itemv1.HandInRequest{
		Actor:   &itemv1.Actor{AccountId: call.Account.String(), CharacterId: call.Character.String()},
		QuestId: "bounty", Completion: 2,
		Take: []*itemv1.HandInTake{{TemplateId: "slime_gel", Quantity: 2}},
		Give: []*itemv1.HandInGive{{TemplateId: "leather_cap", Quantity: 1, Stats: &itemv1.ItemStats{PhysicalDefense: 3}}, {TemplateId: "red_potion", Quantity: 3}},
	}
	if len(f.handIns) != 1 || !proto.Equal(f.handIns[0], want) {
		t.Fatalf("sent %v, want %v", f.handIns, want)
	}
	if got.Revision != 9 || len(got.Slots) != 2 || got.Slots[0].Item != nil || got.Slots[1].Item.ID != gel || got.Slots[1].Item.Quantity != 3 {
		t.Fatalf("got %+v", got)
	}
	refused := grpcx.ErrorFor(codes.FailedPrecondition, grpcx.Key(itemv1.Reason_REASON_NOT_ENOUGH_ITEMS))
	_, err = clients.NewEquipment(itemClient(t, &items{err: refused})).HandIn(context.Background(), call)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(itemv1.Reason_REASON_NOT_ENOUGH_ITEMS) {
		t.Fatalf("a refusal arrived as %v; want its reason kept", err)
	}
	_, err = clients.NewEquipment(itemClient(t, &items{err: status.Error(codes.Unavailable, "down")})).HandIn(context.Background(), call)
	if !errors.Is(err, transient.ErrUnavailable) {
		t.Fatalf("an outage arrived as %v; want it transient", err)
	}
}
