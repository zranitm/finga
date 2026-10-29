package clients_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/model"
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
	gel := kernel.NewItemID()
	f := &items{changed: []*itemv1.Slot{{Tab: "etc", Slot: 2, Item: &itemv1.Item{Id: gel.String(), TemplateId: "slime_gel", Tab: "etc", Slot: 2, Quantity: 7}}, {Tab: "etc", Slot: 4}}}
	p := clients.NewPickups(itemClient(t, f))
	in := model.Pickup{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID(), DropID: uuid.New(), Template: "slime_gel", Quantity: 2}
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
	want := []model.Slot{{Tab: gamedata.Etc, Slot: 2, Item: &model.SlotItem{ID: gel, Template: "slime_gel", Quantity: 7}}, {Tab: gamedata.Etc, Slot: 4}}
	if len(slots) != 2 || slots[0].Tab != want[0].Tab || *slots[0].Item != *want[0].Item || slots[1].Item != nil || slots[1].Slot != 4 {
		t.Fatalf("slots %+v", slots)
	}
}

func TestPickupsRetryOnlyWhatMayPass(t *testing.T) {
	p := clients.NewPickups(nil)
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

// characters is the character service's saves, faked behind a real server.
type characters struct {
	characterv1.UnimplementedCharacterServiceServer
	checkpoints []*characterv1.SaveCheckpointRequest
	levelUps    []*characterv1.SaveLevelUpRequest
}

func (f *characters) SaveCheckpoint(_ context.Context, req *characterv1.SaveCheckpointRequest) (*characterv1.SaveCheckpointResponse, error) {
	f.checkpoints = append(f.checkpoints, req)
	return &characterv1.SaveCheckpointResponse{}, nil
}

func (f *characters) SaveLevelUp(_ context.Context, req *characterv1.SaveLevelUpRequest) (*characterv1.SaveLevelUpResponse, error) {
	f.levelUps = append(f.levelUps, req)
	return &characterv1.SaveLevelUpResponse{}, nil
}

func TestSaverOverTheCharacterService(t *testing.T) {
	f := &characters{}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	characterv1.RegisterCharacterServiceServer(srv, f)
	s := clients.NewSaver(characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv)))
	id := kernel.NewCharacterID()
	ctx := context.Background()
	if err := s.SaveCheckpoint(ctx, model.Checkpoint{ID: id, MapID: "cave", X: 1.5, Y: 2, HP: 3, MP: 4, Exp: 5}); err != nil {
		t.Fatal(err)
	}
	if cp := f.checkpoints[0]; cp.GetCharacterId() != id.String() || cp.GetMapId() != "cave" || cp.GetX() != 1.5 || cp.GetY() != 2 || cp.GetHp() != 3 || cp.GetMp() != 4 || cp.GetExp() != 5 {
		t.Fatalf("checkpoint %+v", cp)
	}
	if err := s.SaveLevelUp(ctx, model.LevelUpSave{ID: id, Level: 4, Exp: 6, Stats: gamedata.Stats{STR: 21, VIT: 16}, HP: 280, MP: 31}); err != nil {
		t.Fatal(err)
	}
	if lu := f.levelUps[0]; lu.GetLevel() != 4 || lu.GetExp() != 6 || lu.GetStats().GetStrength() != 21 || lu.GetStats().GetVitality() != 16 || lu.GetHp() != 280 || lu.GetMp() != 31 {
		t.Fatalf("level-up %+v", lu)
	}
	if s.Retryable(grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)) || !s.Retryable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("only a failure without a reason is retried")
	}
}

// loads is the character service's Load, faked behind a real server.
type loads struct {
	characterv1.UnimplementedCharacterServiceServer
	char *characterv1.Character
	req  *characterv1.LoadRequest
}

func (f *loads) Load(_ context.Context, req *characterv1.LoadRequest) (*characterv1.LoadResponse, error) {
	f.req = req
	if f.char == nil {
		return nil, grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
	}
	return &characterv1.LoadResponse{Character: f.char}, nil
}

func TestCharactersLoadsAsTheWorldJoinsThem(t *testing.T) {
	id, account := kernel.NewCharacterID(), kernel.NewAccountID()
	f := &loads{char: &characterv1.Character{Id: id.String(), Name: "Ayla", Class: "bowman", Level: 3, Exp: 40,
		Stats: &characterv1.Stats{Strength: 5, Dexterity: 18, Intelligence: 4, Agility: 14, Vitality: 5}, Hp: 90, Mp: 30, MapId: "cave", X: 12.5, Y: 600}}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	characterv1.RegisterCharacterServiceServer(srv, f)
	c := clients.NewCharacters(characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv)))
	got, err := c.Load(context.Background(), id, account)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Character{ID: id, Account: account, Name: "Ayla", Class: kernel.Bowman, Level: 3, Exp: 40,
		Stats: gamedata.Stats{STR: 5, DEX: 18, INT: 4, AGI: 14, VIT: 5}, HP: 90, MP: 30, MapID: "cave", X: 12.5, Y: 600}
	if got != want || f.req.GetAccountId() != account.String() || f.req.GetCharacterId() != id.String() {
		t.Fatalf("got %+v from %+v", got, f.req)
	}
	f.char = nil
	_, err = c.Load(context.Background(), id, account)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(characterv1.Reason_REASON_CHARACTER_NOT_FOUND) {
		t.Fatalf("not found: %v", err)
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
