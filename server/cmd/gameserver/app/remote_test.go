package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

// items is the item service, faked behind a real gRPC server.
type items struct {
	itemv1.UnimplementedItemServiceServer
	mu      sync.Mutex
	adds    []*itemv1.AddRequest
	grants  []*itemv1.GrantStarterRequest
	changed []*itemv1.Slot
	err     error
}

func (f *items) Add(_ context.Context, req *itemv1.AddRequest) (*itemv1.AddResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, req)
	return &itemv1.AddResponse{Changed: f.changed}, f.err
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
	gel := model.NewItemID()
	f := &items{changed: []*itemv1.Slot{{Tab: "etc", Slot: 2, Item: &itemv1.Item{Id: gel.String(), TemplateId: "slime_gel", Tab: "etc", Slot: 2, Quantity: 7}}, {Tab: "etc", Slot: 4}}}
	p := pickups{c: itemClient(t, f)}
	in := world.Pickup{Account: model.NewAccountID(), Character: model.NewCharacterID(), DropID: uuid.New(), Template: "slime_gel", Quantity: 2}
	slots, err := p.Add(context.Background(), in)
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
	want := []world.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &world.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}}
	if len(slots) != 2 || slots[0].Tab != want[0].Tab || *slots[0].Item != *want[0].Item || slots[1].Item != nil || slots[1].Slot != 4 {
		t.Fatalf("slots %+v", slots)
	}
}

func TestPickupsRetryOnlyWhatMayPass(t *testing.T) {
	p := pickups{}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a full inventory is a rule", grpcx.Error(codes.FailedPrecondition, itemv1.Reason_REASON_INVENTORY_FULL), false},
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

func TestStarterAsksForTheCharactersClass(t *testing.T) {
	f := &items{}
	s := starter{c: itemClient(t, f)}
	account, id := model.NewAccountID(), model.NewCharacterID()
	if err := s.GrantStarter(context.Background(), account, id, model.Bowman); err != nil {
		t.Fatal(err)
	}
	if g := f.grants[0]; g.GetClass() != "bowman" || g.GetActor().GetAccountId() != account.String() || g.GetActor().GetCharacterId() != id.String() {
		t.Fatalf("grant %+v", g)
	}
	f.err = grpcx.Error(codes.InvalidArgument, itemv1.Reason_REASON_UNKNOWN_ITEM)
	err := s.GrantStarter(context.Background(), account, id, model.Bowman)
	if err == nil || s.Retryable(err) {
		t.Fatalf("a refusal: %v, retryable %v", err, s.Retryable(err))
	}
	if !s.Retryable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("an unreachable service is not retried")
	}
}
