package clients_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	accountv1 "finga/api/gen/account/v1"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/gateway/internal/clients"
)

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
	g := clients.NewGuard(accountv1.NewAccountServiceClient(grpctest.Conn(t, srv)))

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
