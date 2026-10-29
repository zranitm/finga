package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentinels"
	"finga/services/account/internal/account"
	"finga/services/account/internal/handler"
)

// fakeAccounts records what the handler asked and answers err when set.
type fakeAccounts struct {
	mu        sync.Mutex
	register  account.RegisterInput
	login     account.LoginInput
	token     string
	session   uuid.UUID
	accountID uuid.UUID
	updated   account.Settings
	settingOf kernel.AccountID
	err       error
	calls     int
}

var (
	accountID = uuid.Must(uuid.NewV7())
	sessionID = uuid.Must(uuid.NewV7())
	created   = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
)

func (f *fakeAccounts) call() error { f.mu.Lock(); defer f.mu.Unlock(); f.calls++; return f.err }

func (f *fakeAccounts) Register(_ context.Context, in account.RegisterInput) (account.Account, error) {
	f.register = in
	return account.Account{ID: kernel.AccountID(accountID), Username: in.Username, Settings: account.DefaultSettings(), CreatedAt: created}, f.call()
}

func (f *fakeAccounts) Login(_ context.Context, in account.LoginInput) (string, error) {
	f.login = in
	return "tok-1", f.call()
}

func (f *fakeAccounts) Validate(_ context.Context, token string) (auth.Principal, error) {
	f.token = token
	return auth.Principal{AccountID: accountID, SessionID: sessionID}, f.call()
}

func (f *fakeAccounts) Rotate(_ context.Context, id uuid.UUID) (string, error) {
	f.session = id
	return "tok-2", f.call()
}

func (f *fakeAccounts) Check(_ context.Context, id uuid.UUID) error { f.session = id; return f.call() }

func (f *fakeAccounts) Revoke(_ context.Context, id uuid.UUID) error { f.session = id; return f.call() }

func (f *fakeAccounts) RevokeAll(_ context.Context, id uuid.UUID) error {
	f.accountID = id
	return f.call()
}

func (f *fakeAccounts) Settings(_ context.Context, id kernel.AccountID) (account.Settings, error) {
	f.settingOf = id
	return account.Settings{ControlPreset: account.PresetWASD}, f.call()
}

func (f *fakeAccounts) UpdateSettings(_ context.Context, id kernel.AccountID, s account.Settings) error {
	f.settingOf, f.updated = id, s
	return f.call()
}

func serve(t *testing.T, f *fakeAccounts) (accountv1.AccountServiceClient, *handler.Server) {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	h := handler.New(f, "boot-1")
	accountv1.RegisterAccountServiceServer(srv, h)
	return accountv1.NewAccountServiceClient(grpctest.Conn(t, srv)), h
}

func TestEachCallMapsItsFields(t *testing.T) {
	ctx := context.Background()
	f := &fakeAccounts{}
	c, _ := serve(t, f)

	t.Run("Register", func(t *testing.T) {
		res, err := c.Register(ctx, &accountv1.RegisterRequest{Username: "alice", Password: "correct horse"})
		if err != nil {
			t.Fatal(err)
		}
		a := res.GetAccount()
		if f.register != (account.RegisterInput{Username: "alice", Password: "correct horse"}) || a.GetId() != accountID.String() || a.GetUsername() != "alice" ||
			a.GetSettings().GetControlPreset() != "classic" || !a.GetCreatedAt().AsTime().Equal(created) {
			t.Fatalf("input %+v account %+v", f.register, a)
		}
	})
	t.Run("Login carries the caller's IP for the throttle", func(t *testing.T) {
		res, err := c.Login(ctx, &accountv1.LoginRequest{Username: "alice", Password: "pw", Ip: "10.0.0.9"})
		if err != nil || res.GetToken() != "tok-1" || f.login != (account.LoginInput{Username: "alice", Password: "pw", IP: "10.0.0.9"}) {
			t.Fatalf("token %q input %+v err %v", res.GetToken(), f.login, err)
		}
	})
	t.Run("Validate", func(t *testing.T) {
		res, err := c.Validate(ctx, &accountv1.ValidateRequest{Token: "tok-1"})
		if err != nil || f.token != "tok-1" || res.GetAccountId() != accountID.String() || res.GetSessionId() != sessionID.String() {
			t.Fatalf("res %+v err %v", res, err)
		}
	})
	t.Run("Rotate", func(t *testing.T) {
		res, err := c.Rotate(ctx, &accountv1.RotateRequest{SessionId: sessionID.String()})
		if err != nil || res.GetToken() != "tok-2" || f.session != sessionID {
			t.Fatalf("res %+v err %v", res, err)
		}
	})
	t.Run("Check", func(t *testing.T) {
		if _, err := c.Check(ctx, &accountv1.CheckRequest{SessionId: sessionID.String()}); err != nil || f.session != sessionID {
			t.Fatal(err)
		}
	})
	t.Run("Revoke", func(t *testing.T) {
		if _, err := c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: sessionID.String()}); err != nil || f.session != sessionID {
			t.Fatal(err)
		}
	})
	t.Run("RevokeAll", func(t *testing.T) {
		if _, err := c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: accountID.String()}); err != nil || f.accountID != accountID {
			t.Fatal(err)
		}
	})
	t.Run("GetSettings", func(t *testing.T) {
		res, err := c.GetSettings(ctx, &accountv1.GetSettingsRequest{AccountId: accountID.String()})
		if err != nil || res.GetSettings().GetControlPreset() != "wasd" || f.settingOf != kernel.AccountID(accountID) {
			t.Fatalf("res %+v err %v", res, err)
		}
	})
	t.Run("UpdateSettings", func(t *testing.T) {
		if _, err := c.UpdateSettings(ctx, &accountv1.UpdateSettingsRequest{AccountId: accountID.String(), Settings: &accountv1.Settings{ControlPreset: "wasd"}}); err != nil {
			t.Fatal(err)
		}
		if f.updated.ControlPreset != account.PresetWASD || f.settingOf != kernel.AccountID(accountID) {
			t.Fatalf("updated %+v for %v", f.updated, f.settingOf)
		}
	})
}

func TestAMalformedIDNeverReachesTheModule(t *testing.T) {
	f := &fakeAccounts{}
	c, _ := serve(t, f)
	ctx := context.Background()
	calls := []func() error{
		func() error { _, err := c.Rotate(ctx, &accountv1.RotateRequest{SessionId: "x"}); return err },
		func() error { _, err := c.Check(ctx, &accountv1.CheckRequest{}); return err },
		func() error { _, err := c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: "nope"}); return err },
		func() error { _, err := c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: "1"}); return err },
		func() error { _, err := c.GetSettings(ctx, &accountv1.GetSettingsRequest{AccountId: ""}); return err },
		func() error {
			_, err := c.UpdateSettings(ctx, &accountv1.UpdateSettingsRequest{AccountId: "?", Settings: &accountv1.Settings{ControlPreset: "wasd"}})
			return err
		},
	}
	for i, call := range calls {
		if err := call(); status.Code(err) != codes.InvalidArgument || grpcx.IsRule(err) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if f.calls != 0 {
		t.Fatalf("the module was called %d times", f.calls)
	}
}

func TestEveryModuleErrorArrivesWithItsReason(t *testing.T) {
	for _, row := range handler.Errors {
		t.Run(grpcx.Key(row.Reason).Name, func(t *testing.T) {
			c, _ := serve(t, &fakeAccounts{err: row.Err})
			_, err := c.Login(context.Background(), &accountv1.LoginRequest{Username: "a", Password: "b"})
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(row.Reason) || status.Code(err) != row.Code {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "account"))
	if err != nil || len(declared) == 0 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentinels.Referenced("errors.go")
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
		used[grpcx.Key(row.Reason)] = true
	}
	for _, r := range grpcx.Reasons(accountv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}

func TestWatchSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, h := serve(t, &fakeAccounts{})
	watch := func(gateway string) grpc.ServerStreamingClient[accountv1.WatchSessionsResponse] {
		t.Helper()
		stream, err := c.WatchSessions(ctx, &accountv1.WatchSessionsRequest{GatewayId: gateway})
		if err != nil {
			t.Fatal(err)
		}
		m, err := stream.Recv()
		if err != nil || m.GetSubscribed().GetBootId() != "boot-1" {
			t.Fatalf("first message %+v, err %v", m, err)
		}
		return stream
	}
	recv := func(stream grpc.ServerStreamingClient[accountv1.WatchSessionsResponse]) *accountv1.WatchSessionsResponse {
		t.Helper()
		m, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, b := watch("gw-a"), watch("gw-b")

	t.Run("a session ended by token reuse reaches every gateway", func(t *testing.T) {
		h.SessionRevoked(ctx, sessionID, account.ReasonTokenReuse)
		for _, s := range []grpc.ServerStreamingClient[accountv1.WatchSessionsResponse]{a, b} {
			ev := recv(s).GetSessionRevoked()
			if ev.GetSessionId() != sessionID.String() || ev.GetReason() != accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE {
				t.Fatalf("event %+v", ev)
			}
		}
	})
	t.Run("a new login reaches every gateway with its reason", func(t *testing.T) {
		h.AccountRevoked(ctx, accountID, account.ReasonNewLogin)
		for _, s := range []grpc.ServerStreamingClient[accountv1.WatchSessionsResponse]{a, b} {
			ev := recv(s).GetAccountRevoked()
			if ev.GetAccountId() != accountID.String() || ev.GetReason() != accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN {
				t.Fatalf("event %+v", ev)
			}
		}
	})
	t.Run("a logout says so", func(t *testing.T) {
		h.AccountRevoked(ctx, accountID, account.ReasonLogout)
		if got := recv(a).GetAccountRevoked().GetReason(); got != accountv1.RevokeReason_REVOKE_REASON_LOGOUT {
			t.Fatalf("reason %v", got)
		}
		if got := recv(b).GetAccountRevoked().GetReason(); got != accountv1.RevokeReason_REVOKE_REASON_LOGOUT {
			t.Fatalf("reason %v", got)
		}
	})
	t.Run("subscribing again under the same gateway ID ends the older stream", func(t *testing.T) {
		fresh := watch("gw-a")
		if _, err := a.Recv(); status.Code(err) != codes.Unavailable {
			t.Fatalf("the older stream got %v", err)
		}
		h.SessionRevoked(ctx, sessionID, account.ReasonLogout)
		if recv(fresh).GetSessionRevoked() == nil {
			t.Fatal("the new stream missed the event")
		}
	})
	t.Run("a stream without a gateway ID is refused", func(t *testing.T) {
		stream, err := c.WatchSessions(ctx, &accountv1.WatchSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("got %v", err)
		}
	})
}
