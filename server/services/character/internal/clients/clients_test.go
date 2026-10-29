package clients_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/character/internal/clients"
)

// items is the item service's GrantStarter, faked behind a real gRPC server.
type items struct {
	itemv1.UnimplementedItemServiceServer
	grants []*itemv1.GrantStarterRequest
	err    error
}

func (f *items) GrantStarter(_ context.Context, req *itemv1.GrantStarterRequest) (*itemv1.GrantStarterResponse, error) {
	f.grants = append(f.grants, req)
	return &itemv1.GrantStarterResponse{}, f.err
}

func TestStarterAsksForTheCharactersClass(t *testing.T) {
	f := &items{}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	itemv1.RegisterItemServiceServer(srv, f)
	s := clients.NewStarter(itemv1.NewItemServiceClient(grpctest.Conn(t, srv)))
	account, id := kernel.NewAccountID(), kernel.NewCharacterID()
	if err := s.GrantStarter(context.Background(), account, id, kernel.Bowman); err != nil {
		t.Fatal(err)
	}
	if g := f.grants[0]; g.GetClass() != "bowman" || g.GetActor().GetAccountId() != account.String() || g.GetActor().GetCharacterId() != id.String() {
		t.Fatalf("grant %+v", g)
	}
	f.err = grpcx.Error(codes.InvalidArgument, itemv1.Reason_REASON_UNKNOWN_ITEM)
	err := s.GrantStarter(context.Background(), account, id, kernel.Bowman)
	if err == nil || s.Retryable(err) {
		t.Fatalf("a refusal: %v, retryable %v", err, s.Retryable(err))
	}
	if !s.Retryable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("an unreachable service is not retried")
	}
}
