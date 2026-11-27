package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	model "finga/internal/model/account"
	"finga/internal/shared/facade"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace/noop"
)

// fakeCases records what the facade passed in and answers err when set.
type fakeCases struct {
	in       any
	id       any
	settings model.Settings
	err      error
}

var (
	accountID = kernel.NewAccountID()
	sessionID = kernel.NewSessionID()
	created   = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
)

func (f *fakeCases) Register(_ context.Context, in RegisterInput) (model.Account, error) {
	f.in = in
	return model.Account{ID: accountID, Username: in.Username, Settings: model.Settings{ChatPinned: true}, CreatedAt: created}, f.err
}

func (f *fakeCases) Login(_ context.Context, in LoginInput) (string, error) {
	f.in = in
	return "tok-1", f.err
}

func (f *fakeCases) Validate(_ context.Context, token string) (model.Principal, error) {
	f.in = token
	return model.Principal{AccountID: accountID, SessionID: sessionID}, f.err
}

func (f *fakeCases) Rotate(_ context.Context, id kernel.SessionID) (string, error) {
	f.id = id
	return "tok-2", f.err
}

func (f *fakeCases) Check(_ context.Context, id kernel.SessionID) error  { f.id = id; return f.err }
func (f *fakeCases) Revoke(_ context.Context, id kernel.SessionID) error { f.id = id; return f.err }

func (f *fakeCases) RevokeAll(_ context.Context, id kernel.AccountID) error {
	f.id = id
	return f.err
}

func (f *fakeCases) Settings(_ context.Context, id kernel.AccountID) (model.Settings, error) {
	f.id = id
	return model.Settings{AutoAssignPoints: true, Keys: map[string]string{"attack": "KeyV"}}, f.err
}

func (f *fakeCases) UpdateSettings(_ context.Context, id kernel.AccountID, s model.Settings) error {
	f.id, f.settings = id, s
	return f.err
}

func newModule(t *testing.T, svc useCases) *Module {
	t.Helper()
	calls, err := facade.New(facade.Options{Module: "account", Traces: noop.NewTracerProvider(), Registry: prometheus.NewRegistry(), Panics: panics.NewRecorder(slog.New(slog.DiscardHandler), nil), Errors: errorTable})
	if err != nil {
		t.Fatal(err)
	}
	return &Module{calls: calls, svc: svc, sessions: hub.New[Event](sessionsQueue)}
}

func TestEachCallConvertsToAndFromTheModel(t *testing.T) {
	ctx := t.Context()
	f := &fakeCases{}
	m := newModule(t, f)
	t.Run("Register", func(t *testing.T) {
		a, err := m.Register(ctx, Credentials{Username: "alice", Password: "correct horse"})
		want := Account{ID: accountID, Username: "alice", Settings: Settings{ChatPinned: true}, CreatedAt: created}
		if err != nil || !reflect.DeepEqual(a, want) || f.in != (RegisterInput{Username: "alice", Password: "correct horse"}) {
			t.Fatalf("got %+v from %+v, err %v", a, f.in, err)
		}
	})
	t.Run("Login", func(t *testing.T) {
		token, err := m.Login(ctx, Credentials{Username: "alice", Password: "pw"})
		if err != nil || token != "tok-1" || f.in != (LoginInput{Username: "alice", Password: "pw"}) {
			t.Fatalf("got %q from %+v, err %v", token, f.in, err)
		}
	})
	t.Run("Validate", func(t *testing.T) {
		p, err := m.Validate(ctx, "tok-1")
		if err != nil || p != (Principal{AccountID: accountID, SessionID: sessionID}) || f.in != "tok-1" {
			t.Fatalf("got %+v, err %v", p, err)
		}
	})
	t.Run("Rotate", func(t *testing.T) {
		token, err := m.Rotate(ctx, sessionID)
		if err != nil || token != "tok-2" || f.id != sessionID {
			t.Fatalf("got %q, err %v", token, err)
		}
	})
	t.Run("Check, Revoke and RevokeAll pass their IDs", func(t *testing.T) {
		if err := m.Check(ctx, sessionID); err != nil || f.id != sessionID {
			t.Fatalf("Check: %v", err)
		}
		if err := m.Revoke(ctx, sessionID); err != nil || f.id != sessionID {
			t.Fatalf("Revoke: %v", err)
		}
		if err := m.RevokeAll(ctx, accountID); err != nil || f.id != accountID {
			t.Fatalf("RevokeAll: %v", err)
		}
	})
	t.Run("Settings", func(t *testing.T) {
		s, err := m.Settings(ctx, accountID)
		if err != nil || !reflect.DeepEqual(s, Settings{AutoAssignPoints: true, Keys: map[string]string{"attack": "KeyV"}}) || f.id != accountID {
			t.Fatalf("got %+v, err %v", s, err)
		}
	})
	t.Run("UpdateSettings", func(t *testing.T) {
		err := m.UpdateSettings(ctx, accountID, Settings{ChatPinned: true, Keys: map[string]string{"skill1": "KeyV"}})
		if err != nil || !reflect.DeepEqual(f.settings, model.Settings{ChatPinned: true, Keys: map[string]string{"skill1": "KeyV"}}) || f.id != accountID {
			t.Fatalf("got %+v, err %v", f.settings, err)
		}
	})
}

// Callers find the kernel error with errors.Is, and the logs keep the model's cause.
func TestEveryModelErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, row := range errorTable {
		t.Run(row.Model.Error(), func(t *testing.T) {
			m := newModule(t, &fakeCases{err: fmt.Errorf("login: %w", row.Model)})
			_, err := m.Login(t.Context(), Credentials{Username: "alice", Password: "pw"})
			if !errors.Is(err, row.Kernel) || !errors.Is(err, row.Model) {
				t.Fatalf("got %v, want %v wrapping %v", err, row.Kernel, row.Model)
			}
		})
	}
	m := newModule(t, &fakeCases{err: model.ErrNotFound})
	if err := m.Check(t.Context(), sessionID); !errors.Is(err, kernel.ErrAccountNotFound) {
		t.Fatalf("an unknown account: got %v", err)
	}
}

// Every subscribed gateway hears each ended session the module tells of, with why, in order.
func TestASubscribedGatewayHearsOfEachEndedSession(t *testing.T) {
	m := newModule(t, &fakeCases{})
	n := notifier{sessions: m.sessions}
	a, b := m.Subscribe("gw-a"), m.Subscribe("gw-b")
	n.SessionRevoked(t.Context(), sessionID, ReasonTokenReuse)
	n.AccountRevoked(t.Context(), accountID, ReasonNewLogin)
	n.AccountRevoked(t.Context(), accountID, ReasonLogout)
	want := []Event{
		SessionRevoked{Session: sessionID, Reason: RevokeTokenReuse},
		Revoked{Account: accountID, Reason: RevokeNewLogin},
		Revoked{Account: accountID, Reason: RevokeLogout},
	}
	for _, sub := range []*hub.Sub[Event]{a, b} {
		for _, w := range want {
			if got := <-sub.C(); got != w {
				t.Fatalf("got %+v, want %+v", got, w)
			}
		}
	}
	if fresh := m.Subscribe("gw-a"); fresh == a {
		t.Fatal("a second subscription is the first")
	}
	select {
	case <-a.Done():
	default:
		t.Fatal("subscribing again under the same gateway left the older subscription open")
	}
}
