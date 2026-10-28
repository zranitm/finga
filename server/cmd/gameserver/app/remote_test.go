package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
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
	changed []*itemv1.Slot
	err     error
}

func (f *items) Add(_ context.Context, req *itemv1.AddRequest) (*itemv1.AddResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds = append(f.adds, req)
	return &itemv1.AddResponse{Changed: f.changed}, f.err
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

// accounts is the account service, faked behind a real gRPC server.
type accounts struct {
	accountv1.UnimplementedAccountServiceServer
	seen []string // what each call carried
	err  error
}

func (f *accounts) Validate(_ context.Context, req *accountv1.ValidateRequest) (*accountv1.ValidateResponse, error) {
	f.seen = append(f.seen, "validate "+req.GetToken())
	return &accountv1.ValidateResponse{AccountId: guardAccount.String(), SessionId: guardSession.String()}, f.err
}

func (f *accounts) Rotate(_ context.Context, req *accountv1.RotateRequest) (*accountv1.RotateResponse, error) {
	f.seen = append(f.seen, "rotate "+req.GetSessionId())
	return &accountv1.RotateResponse{Token: "next"}, f.err
}

func (f *accounts) Check(_ context.Context, req *accountv1.CheckRequest) (*accountv1.CheckResponse, error) {
	f.seen = append(f.seen, "check "+req.GetSessionId())
	return &accountv1.CheckResponse{}, f.err
}

func (f *accounts) Revoke(_ context.Context, req *accountv1.RevokeRequest) (*accountv1.RevokeResponse, error) {
	f.seen = append(f.seen, "revoke "+req.GetSessionId())
	return &accountv1.RevokeResponse{}, f.err
}

func (f *accounts) RevokeAll(_ context.Context, req *accountv1.RevokeAllRequest) (*accountv1.RevokeAllResponse, error) {
	f.seen = append(f.seen, "revoke all "+req.GetAccountId())
	return &accountv1.RevokeAllResponse{}, f.err
}

var (
	guardAccount = uuid.Must(uuid.NewV7())
	guardSession = uuid.Must(uuid.NewV7())
)

func TestGuardOverTheAccountService(t *testing.T) {
	ctx := context.Background()
	f := &accounts{}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	accountv1.RegisterAccountServiceServer(srv, f)
	g := guard{c: accountv1.NewAccountServiceClient(grpctest.Conn(t, srv))}

	p, err := g.Validate(ctx, "tok")
	if err != nil || p.AccountID != guardAccount || p.SessionID != guardSession {
		t.Fatalf("principal %+v, err %v", p, err)
	}
	if token, err := g.Rotate(ctx, guardSession); err != nil || token != "next" {
		t.Fatalf("rotate %q %v", token, err)
	}
	if err := g.Check(ctx, guardSession); err != nil {
		t.Fatal(err)
	}
	if err := g.Revoke(ctx, guardSession); err != nil {
		t.Fatal(err)
	}
	if err := g.RevokeAll(ctx, guardAccount); err != nil {
		t.Fatal(err)
	}
	want := []string{"validate tok", "rotate " + guardSession.String(), "check " + guardSession.String(), "revoke " + guardSession.String(), "revoke all " + guardAccount.String()}
	if strings.Join(f.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", f.seen)
	}
	t.Run("an unauthenticated answer keeps its reason", func(t *testing.T) {
		f.err = grpcx.Error(codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED)
		_, err := g.Validate(ctx, "bad")
		if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(accountv1.Reason_REASON_UNAUTHENTICATED) {
			t.Fatalf("got %v", err)
		}
	})
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
	s := saver{c: characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv))}
	id := model.NewCharacterID()
	ctx := context.Background()
	if err := s.SaveCheckpoint(ctx, world.Checkpoint{ID: id, MapID: "cave", X: 1.5, Y: 2, HP: 3, MP: 4, Exp: 5}); err != nil {
		t.Fatal(err)
	}
	if cp := f.checkpoints[0]; cp.GetCharacterId() != id.String() || cp.GetMapId() != "cave" || cp.GetX() != 1.5 || cp.GetY() != 2 || cp.GetHp() != 3 || cp.GetMp() != 4 || cp.GetExp() != 5 {
		t.Fatalf("checkpoint %+v", cp)
	}
	if err := s.SaveLevelUp(ctx, world.LevelUpSave{ID: id, Level: 4, Exp: 6, Stats: gamedata.Stats{STR: 21, VIT: 16}, HP: 280, MP: 31}); err != nil {
		t.Fatal(err)
	}
	if lu := f.levelUps[0]; lu.GetLevel() != 4 || lu.GetExp() != 6 || lu.GetStats().GetStrength() != 21 || lu.GetStats().GetVitality() != 16 || lu.GetHp() != 280 || lu.GetMp() != 31 {
		t.Fatalf("level-up %+v", lu)
	}
	if s.Retryable(grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)) || !s.Retryable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("only a failure without a reason is retried")
	}
}
