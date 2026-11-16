package account_test

import (
	"errors"
	"log/slog"
	"testing"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/modules/account"
	"finga/modules/account/internal/model"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
)

// Each error a call meets reaches callers as its kernel error, with its reason.
// The rows are literals, so a row changed in the facade's table or a reason changed in the kernel fails here.
func TestEachErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, tt := range []struct {
		err    error
		kernel error  // nil for none
		reason string // "" for none
		retry  bool   // a retry keyed on transient.ErrUnavailable retries it
	}{
		{model.ErrNotFound, kernel.ErrAccountNotFound, "account_not_found", false},
		{model.ErrUsernameTaken, kernel.ErrUsernameTaken, "username_taken", false},
		{model.ErrUnauthenticated, kernel.ErrUnauthenticated, "unauthenticated", false},
		{model.ErrRateLimited, kernel.ErrLoginRateLimited, "login_rate_limited", false},
		{model.ErrBadUsername, kernel.ErrBadUsername, "bad_username", false},
		{model.ErrBadPassword, kernel.ErrBadPassword, "bad_password", false},
		{model.ErrBadPreset, kernel.ErrBadControlPreset, "bad_control_preset", false},
		{model.ErrSessionRace, kernel.ErrSessionRace, "session_race", false},
		{kernel.ErrBadID, kernel.ErrBadID, "bad_id", false},
		{model.ErrBusy, kernel.ErrBusy, "busy", true},
		{transient.ErrUnavailable, nil, "", true},
		{errors.New("disk on fire"), nil, "", false}, // passes as it is, so a caller reads it as internal
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			err := call(t, func() error { return tt.err })
			if tt.kernel != nil && !errors.Is(err, tt.kernel) || !errors.Is(err, tt.err) {
				t.Fatalf("reached callers as %v; want %v wrapping %v", err, tt.kernel, tt.err)
			}
			if reason, ok := kernel.ReasonOf(err); reason != tt.reason || ok != (tt.reason != "") {
				t.Fatalf("reason %q (%v), want %q", reason, ok, tt.reason)
			}
			if retry := errors.Is(err, transient.ErrUnavailable); retry != tt.retry {
				t.Fatalf("a caller retrying on transient.ErrUnavailable retries it: %v, want %v", retry, tt.retry)
			}
		})
	}
}

// A panic in a call reaches callers as kernel.ErrInternal, with no reason.
func TestAPanicIsInternal(t *testing.T) {
	err := call(t, func() error { panic("till jammed") })
	if _, has := kernel.ReasonOf(err); !errors.Is(err, kernel.ErrInternal) || has {
		t.Fatalf("reached callers as %v", err)
	}
}

// call is the error a call that met do's returns, through the module's facade as a caller in the process gets it.
func call(t *testing.T, do func() error) (err error) {
	t.Helper()
	h, hostErr := host.New(host.Options{Name: "finga", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	t.Cleanup(h.Close)
	mod, hostErr := h.Module("account")
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	calls, hostErr := mod.Calls(account.ErrorTable)
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	_, c := calls.Start(t.Context(), "Call")
	defer c.End(&err)
	return do()
}

// Each reason the module ends sessions for reaches a subscribed gateway by name, and one it has no name for as RevokeUnspecified.
func TestEachRevokeReasonReachesTheGateway(t *testing.T) {
	m, notes := account.Streams()
	sub := m.Subscribe("gw")
	for _, tt := range []struct {
		reason string
		want   account.RevokeReason
	}{
		{"token reuse", account.RevokeTokenReuse},
		{"logged out", account.RevokeLogout},
		{"logged in elsewhere", account.RevokeNewLogin},
		{"a reason the module has no name for", account.RevokeUnspecified},
	} {
		session, acc := kernel.NewSessionID(), kernel.NewAccountID()
		notes.SessionRevoked(t.Context(), session, tt.reason)
		notes.AccountRevoked(t.Context(), acc, tt.reason)
		if got := <-sub.C(); got != (account.SessionRevoked{Session: session, Reason: tt.want}) {
			t.Fatalf("%q: a session's end went out as %+v; want %v", tt.reason, got, tt.want)
		}
		if got := <-sub.C(); got != (account.Revoked{Account: acc, Reason: tt.want}) {
			t.Fatalf("%q: an account's went out as %+v; want %v", tt.reason, got, tt.want)
		}
	}
}
