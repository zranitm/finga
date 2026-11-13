package account_test

import (
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/hub"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/account"
	"finga/services/account/internal/application"
	"finga/services/account/internal/handler"
	"finga/services/account/internal/model"
)

// Each error a call meets leaves the service with the code and reason the contract gave it before the module had a facade.
// The rows are literals, so a row changed in either the facade's table or the handler's fails here.
func TestEachErrorKeepsItsWire(t *testing.T) {
	for _, tt := range []struct {
		err    error
		code   codes.Code
		reason protoreflect.Enum // nil for none
		retry  bool              // a retry keyed on transient.ErrUnavailable retries it
	}{
		{model.ErrNotFound, codes.NotFound, accountv1.Reason_REASON_ACCOUNT_NOT_FOUND, false},
		{model.ErrUsernameTaken, codes.AlreadyExists, accountv1.Reason_REASON_USERNAME_TAKEN, false},
		{model.ErrUnauthenticated, codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED, false},
		{model.ErrRateLimited, codes.ResourceExhausted, accountv1.Reason_REASON_LOGIN_RATE_LIMITED, false},
		{model.ErrBadUsername, codes.InvalidArgument, accountv1.Reason_REASON_BAD_USERNAME, false},
		{model.ErrBadPassword, codes.InvalidArgument, accountv1.Reason_REASON_BAD_PASSWORD, false},
		{model.ErrBadPreset, codes.InvalidArgument, accountv1.Reason_REASON_BAD_CONTROL_PRESET, false},
		{model.ErrSessionRace, codes.Aborted, accountv1.Reason_REASON_SESSION_RACE, false},
		{kernel.ErrBadID, codes.InvalidArgument, accountv1.Reason_REASON_BAD_ID, false},
		{model.ErrBusy, codes.ResourceExhausted, accountv1.Reason_REASON_BUSY, true},
		{transient.ErrUnavailable, codes.Unavailable, nil, true},
		{errors.New("disk on fire"), codes.Internal, nil, false},
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			code, reason, has, err := wire(t, func() error { return tt.err })
			if code != tt.code || has != (tt.reason != nil) || has && reason != grpcx.Key(tt.reason) {
				t.Fatalf("left as %s %v, want %s %v", code, reason, tt.code, tt.reason)
			}
			if retry := errors.Is(err, transient.ErrUnavailable); retry != tt.retry {
				t.Fatalf("a caller retrying on transient.ErrUnavailable retries it: %v, want %v", retry, tt.retry)
			}
		})
	}
}

// A panic in a call answers as one the gRPC server recovers did: Internal, with the usual text and no reason.
func TestAPanicKeepsItsWire(t *testing.T) {
	code, _, has, _ := wire(t, func() error { panic("till jammed") })
	if code != codes.Internal || has {
		t.Fatalf("left as %s, reason %v", code, has)
	}
}

// wire is the code and reason an error met inside a call leaves the service with, through the module's facade, then the handler's table, and the error the facade returned.
func wire(t *testing.T, do func() error) (codes.Code, grpcx.Reason, bool, error) {
	t.Helper()
	h, err := host.New(host.Options{Name: "account", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("account")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := mod.Calls(account.ErrorTable)
	if err != nil {
		t.Fatal(err)
	}
	err = func() (err error) {
		_, call := calls.Start(t.Context(), "Wire")
		defer call.End(&err)
		return do()
	}()
	st, _ := handler.Errors.Status(err)
	if st.Code() == codes.Internal && st.Message() != "internal error" {
		t.Fatalf("Internal says %q", st.Message())
	}
	reason, has := grpcx.ReasonOf(st.Err())
	return st.Code(), reason, has, err
}

// sessions serves the module's sessions stream through the handler, as a gateway subscribes to it.
type sessions struct {
	handler.Service
	m *account.Module
}

func (s sessions) Subscribe(gateway string) *hub.Sub[account.Event] { return s.m.Subscribe(gateway) }

// Each reason the module ends sessions for reaches the gateway as the contract named it before the module had a facade.
func TestEachRevokeReasonKeepsItsWire(t *testing.T) {
	m, notes := account.Streams()
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	accountv1.RegisterAccountServiceServer(g, handler.New(sessions{m: m}, "boot-1"))
	stream, err := accountv1.NewAccountServiceClient(grpctest.Conn(t, g)).WatchSessions(t.Context(), &accountv1.WatchSessionsRequest{GatewayId: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	if first, err := stream.Recv(); err != nil || first.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %v, %v", first, err)
	}
	for _, tt := range []struct {
		reason string
		want   accountv1.RevokeReason
	}{
		{application.ReasonTokenReuse, accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE},
		{application.ReasonLogout, accountv1.RevokeReason_REVOKE_REASON_LOGOUT},
		{application.ReasonNewLogin, accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN},
		{"a reason the contract has no name for", accountv1.RevokeReason_REVOKE_REASON_UNSPECIFIED},
	} {
		session, acc := kernel.NewSessionID(), kernel.NewAccountID()
		notes.SessionRevoked(t.Context(), session, tt.reason)
		notes.AccountRevoked(t.Context(), acc, tt.reason)
		one, err := stream.Recv()
		if r := one.GetSessionRevoked(); err != nil || r.GetSessionId() != session.String() || r.GetReason() != tt.want {
			t.Fatalf("%q: a session's end went out as %v, %v; want %v", tt.reason, one, err, tt.want)
		}
		all, err := stream.Recv()
		if r := all.GetAccountRevoked(); err != nil || r.GetAccountId() != acc.String() || r.GetReason() != tt.want {
			t.Fatalf("%q: an account's went out as %v, %v; want %v", tt.reason, all, err, tt.want)
		}
	}
}
