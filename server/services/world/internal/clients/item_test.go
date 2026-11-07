package clients_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/uuidv7"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/model"
)

type items struct {
	itemv1.UnimplementedItemServiceServer
	mu       sync.Mutex
	adds     []*itemv1.AddRequest
	grants   []*itemv1.GrantStarterRequest
	changed  []*itemv1.Slot
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

func (f *items) Add(_ context.Context, req *itemv1.AddRequest) (*itemv1.AddResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, req)
	return &itemv1.AddResponse{Changed: f.changed, Revision: f.revision}, f.err
}

func (f *items) GrantStarter(_ context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, req)
	return &itemv1.GrantStarterResponse{}, f.err
}

func itemClient(t *testing.T, f *items) itemv1.ItemServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	itemv1.RegisterItemServiceServer(srv, f)
	return itemv1.NewItemServiceClient(grpctest.Conn(t, srv))
}

func TestPickupsAddTheDropUnderItsOwnKey(t *testing.T) {
	gel := kernel.NewItemID()
	f := &items{changed: []*itemv1.Slot{{Tab: "etc", Slot: 2, Item: &itemv1.Item{Id: gel.String(), TemplateId: "slime_gel", Tab: "etc", Slot: 2, Quantity: 7}}, {Tab: "etc", Slot: 4}}, revision: 31}
	p := clients.NewPickups(itemClient(t, f))
	in := model.Pickup{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), DropID: uuidv7.New(), Template: "slime_gel", Quantity: 2}
	changed, err := p.Add(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	req := f.adds[0]
	if req.GetIdempotencyKey() != in.DropID.String() || req.GetCause().GetId() != in.DropID.String() || req.GetCause().GetType() != itemv1.CauseType_CAUSE_TYPE_DROP {
		t.Fatalf("the drop ID is not both the key and the cause: %+v", req)
	}
	if req.GetActor().GetAccountId() != in.Account.String() || req.GetActor().GetCharacterId() != in.Character.String() || req.GetTemplateId() != "slime_gel" || req.GetQuantity() != 2 {
		t.Fatalf("request %+v", req)
	}
	want := []model.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}}
	if slots := changed.Slots; len(slots) != 2 || slots[0].Tab != want[0].Tab || *slots[0].Item != *want[0].Item || slots[1].Item != nil || slots[1].Slot != 4 || changed.Revision != 31 {
		t.Fatalf("changed %+v, want the two slots at revision 31", changed)
	}
}

func TestPickupsRetryOnlyWhatMayPass(t *testing.T) {
	p := clients.NewPickups(nil)
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

// A key that already served another fingerprint means an earlier try granted the drop.
func TestPickupsKnowAGrantByItsReusedKey(t *testing.T) {
	p := clients.NewPickups(nil)
	if !p.Granted(grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED)) {
		t.Fatal("a reused key is not a grant")
	}
	if p.Granted(grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL)) || p.Granted(status.Error(codes.Unavailable, "down")) {
		t.Fatal("another failure counts as a grant")
	}
}

func TestPickupsReadTheWholeInventoryAsSlotsAtItsRevision(t *testing.T) {
	gel, potion, ayla := kernel.NewItemID(), kernel.NewItemID(), kernel.NewCharacterID()
	f := &items{held: []*itemv1.Item{
		{Id: gel.String(), TemplateId: "slime_gel", Tab: "etc", Slot: 2, Quantity: 7},
		{Id: potion.String(), TemplateId: "red_potion", Tab: "use", Slot: 0, Quantity: 5},
	}, revision: 12}
	inv, err := clients.NewPickups(itemClient(t, f)).Inventory(context.Background(), ayla)
	if err != nil {
		t.Fatal(err)
	}
	slots := inv.Slots
	want := []model.Slot{
		{Tab: gamedata.Etc, Slot: 2, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}},
		{Tab: gamedata.Use, Slot: 0, Item: &model.SlotItem{ID: potion, Template: "red_potion", Quantity: 5}},
	}
	if len(f.asked) != 1 || f.asked[0] != ayla.String() || len(slots) != len(want) || inv.Revision != 12 {
		t.Fatalf("asked %v, read %+v; want the two slots at revision 12", f.asked, inv)
	}
	for k := range want {
		if slots[k].Tab != want[k].Tab || slots[k].Slot != want[k].Slot || *slots[k].Item != *want[k].Item {
			t.Fatalf("slot %d = %+v, want %+v", k, slots[k], want[k])
		}
	}
	f.held = []*itemv1.Item{{Id: "nope", TemplateId: "slime_gel", Tab: "etc"}}
	if _, err := clients.NewPickups(itemClient(t, f)).Inventory(context.Background(), ayla); err == nil {
		t.Fatal("an item with a malformed ID was read")
	}
}
