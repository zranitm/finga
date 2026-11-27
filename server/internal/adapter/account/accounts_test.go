package account

import (
	"context"
	"reflect"
	"testing"

	"finga/internal/application/account"
	model "finga/internal/model/account"
	"finga/internal/shared/failure"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
	"finga/pkg/hub"
)

// accountModule records what it was asked and answers with its fields.
type accountModule struct {
	token     string
	principal account.Principal
}

func (m *accountModule) Validate(_ context.Context, token string) (account.Principal, error) {
	m.token = token
	return m.principal, nil
}

func (m *accountModule) Rotate(context.Context, kernel.SessionID) (string, error) { return "", nil }

func (m *accountModule) Check(context.Context, kernel.SessionID) error { return nil }

func (m *accountModule) Subscribe(string) *hub.Sub[account.Event] { return nil }

// A validated token reaches the module, and its principal reaches the guard with every field.
func TestEachGuardCallKeepsItsFields(t *testing.T) {
	m := &accountModule{}
	g := NewGuard(m)
	ctx := t.Context()
	type validate struct {
		Token     string
		Principal account.Principal
	}
	type got struct {
		Token     string
		Principal model.Principal
	}
	diff := copytest.Diff(copytest.Match{}, func(v validate) got {
		m.principal = v.Principal
		out, _ := g.Validate(ctx, v.Token)
		return got{m.token, out}
	})
	if len(diff) > 0 {
		t.Errorf("Validate: %v", diff)
	}
}

// A refused token reaches the guard as the failure a client sees, of the kind the use cases act on.
func TestAGuardNamesAFailedValidate(t *testing.T) {
	m := &accountFailing{err: kernel.ErrUnauthenticated}
	_, err := NewGuard(m).Validate(t.Context(), "tok")
	if p := failure.PublicOf(err); p != failure.Unauthenticated {
		t.Fatalf("validate failed as %v, want %v", p, failure.Unauthenticated)
	}
}

// accountFailing is an account module whose Validate fails.
type accountFailing struct {
	accountModule
	err error
}

func (m *accountFailing) Validate(context.Context, string) (account.Principal, error) {
	return account.Principal{}, m.err
}

// Each ended session reaches the server with what it acts on: the session, or the account and whether a login elsewhere ended it.
func TestEachSessionEndKeepsItsFields(t *testing.T) {
	end := func(e account.Event) model.SessionEnd {
		out, ok := sessionEnd(e)
		if !ok {
			t.Fatalf("%T: no end", e)
		}
		return out
	}
	reason := map[reflect.Type]any{reflect.TypeFor[account.RevokeReason](): account.RevokeTokenReuse}
	revoked := copytest.Match{Skip: map[string]string{"Reason": "one session ends the same to its player whatever ended it"}, Enums: reason}
	if got := copytest.Diff(revoked, func(e account.SessionRevoked) model.SessionEnd { return end(e) }); len(got) > 0 {
		t.Errorf("SessionRevoked: %v", got)
	}
	all := copytest.Match{Computed: map[string]string{"NewLogin": "the one reason told apart, checked below"}, Enums: reason}
	if got := copytest.Diff(all, func(e account.Revoked) model.SessionEnd { return end(e) }); len(got) > 0 {
		t.Errorf("Revoked: %v", got)
	}
	for r, newLogin := range map[account.RevokeReason]bool{
		account.RevokeUnspecified: false, account.RevokeTokenReuse: false, account.RevokeLogout: false, account.RevokeNewLogin: true,
	} {
		if got := end(account.Revoked{Reason: r}).(model.SessionsRevoked); got.NewLogin != newLogin {
			t.Errorf("reason %d: a new login %v", r, got.NewLogin)
		}
	}
}
