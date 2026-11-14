package account

import (
	"testing"

	"finga/modules/account/internal/application"
	"finga/modules/account/internal/model"
	"finga/pkg/copytest"
)

// Each conversion between the facade's types and the model's keeps every field.
func TestEveryConversionKeepsEveryField(t *testing.T) {
	f := &fakeService{}
	m := newModule(t, f)
	for name, got := range map[string][]string{
		"accountOf":      copytest.Diff(copytest.Match{}, accountOf),
		"settingsOf":     copytest.Diff(copytest.Match{}, settingsOf),
		"Settings.model": copytest.Diff(copytest.Match{}, Settings.model),
		"Principal":      copytest.Diff(copytest.Match{}, func(p model.Principal) Principal { return Principal(p) }),
		"Register": copytest.Diff(copytest.Match{}, func(c Credentials) application.RegisterInput {
			_, _ = m.Register(t.Context(), c)
			return f.in.(application.RegisterInput)
		}),
		"Login": copytest.Diff(copytest.Match{}, func(c Credentials) application.LoginInput {
			_, _ = m.Login(t.Context(), c)
			return f.in.(application.LoginInput)
		}),
	} {
		if len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
