package local

import (
	"context"
	"reflect"
	"testing"

	"finga/internal/kernel"
	"finga/modules/account"
	"finga/modules/gateway/internal/model"
	"finga/pkg/copytest"
	"finga/pkg/hub"
)

// accountModule records what it was asked and answers with its fields.
type accountModule struct {
	creds     account.Credentials
	id        kernel.AccountID
	acc       account.Account
	token     string
	principal account.Principal
	settings  account.Settings
}

func (m *accountModule) Register(_ context.Context, c account.Credentials) (account.Account, error) {
	m.creds = c
	return m.acc, nil
}

func (m *accountModule) Login(_ context.Context, c account.Credentials) (string, error) {
	m.creds = c
	return m.token, nil
}

func (m *accountModule) Validate(_ context.Context, token string) (account.Principal, error) {
	m.token = token
	return m.principal, nil
}

func (m *accountModule) Rotate(context.Context, kernel.SessionID) (string, error) { return "", nil }

func (m *accountModule) Check(context.Context, kernel.SessionID) error { return nil }

func (m *accountModule) Revoke(context.Context, kernel.SessionID) error { return nil }

func (m *accountModule) RevokeAll(context.Context, kernel.AccountID) error { return nil }

func (m *accountModule) Settings(_ context.Context, id kernel.AccountID) (account.Settings, error) {
	m.id = id
	return m.settings, nil
}

func (m *accountModule) UpdateSettings(_ context.Context, id kernel.AccountID, s account.Settings) error {
	m.id, m.settings = id, s
	return nil
}

func (m *accountModule) Subscribe(string) *hub.Sub[account.Event] { return nil }

// credentials are a sign-up's or a login's arguments.
type credentials struct{ Username, Password string }

// Each account call reaches the module with every field, and its answer reaches the lobby with every field the lobby takes.
func TestEachAccountCallKeepsItsFields(t *testing.T) {
	m := &accountModule{}
	a, g := NewAccounts(m), NewGuard(m)
	ctx := t.Context()
	for name, diff := range map[string]func() []string{
		"Register's credentials": func() []string {
			return copytest.Diff(copytest.Match{}, func(c credentials) account.Credentials {
				_, _ = a.Register(ctx, c.Username, c.Password)
				return m.creds
			})
		},
		"Register's account": func() []string {
			match := copytest.Match{Skip: map[string]string{"Settings": "a new account's settings are the defaults, which the lobby reads with Settings"}}
			return copytest.Diff(match, func(acc account.Account) model.Account {
				m.acc = acc
				out, _ := a.Register(ctx, "", "")
				return out
			})
		},
		"Login": func() []string {
			type login struct {
				Creds credentials
				Token string
			}
			type sent struct {
				Creds account.Credentials
				Token string
			}
			return copytest.Diff(copytest.Match{}, func(l login) sent {
				m.token = l.Token
				token, _ := a.Login(ctx, l.Creds.Username, l.Creds.Password)
				return sent{m.creds, token}
			})
		},
		"Settings": func() []string {
			type read struct {
				ID       kernel.AccountID
				Settings account.Settings
			}
			type got struct {
				ID       kernel.AccountID
				Settings model.Settings
			}
			return copytest.Diff(copytest.Match{}, func(r read) got {
				m.settings = r.Settings
				out, _ := a.Settings(ctx, r.ID)
				return got{m.id, out}
			})
		},
		"UpdateSettings": func() []string {
			type update struct {
				ID       kernel.AccountID
				Settings model.Settings
			}
			type sent struct {
				ID       kernel.AccountID
				Settings account.Settings
			}
			return copytest.Diff(copytest.Match{}, func(u update) sent {
				_ = a.UpdateSettings(ctx, u.ID, u.Settings)
				return sent{m.id, m.settings}
			})
		},
		"Validate": func() []string {
			type validate struct {
				Token     string
				Principal account.Principal
			}
			type got struct {
				Token     string
				Principal model.Principal
			}
			return copytest.Diff(copytest.Match{}, func(v validate) got {
				m.principal = v.Principal
				out, _ := g.Validate(ctx, v.Token)
				return got{m.token, out}
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// Each ended session reaches the gateway with what it acts on: the session, or the account and whether a login elsewhere ended it.
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
		if got := end(account.Revoked{Reason: r}).(model.AccountRevoked); got.NewLogin != newLogin {
			t.Errorf("reason %d: a new login %v", r, got.NewLogin)
		}
	}
}
