package account

import (
	"testing"

	"finga/pkg/copytest"
)

// A registered account reaches the client with every field it shows.
func TestARegisteredAccountKeepsWhatTheClientShows(t *testing.T) {
	match := copytest.Match{Skip: map[string]string{"Settings": "a new account's settings are the defaults, which the client reads with Settings"}}
	if got := copytest.Diff(match, toAccount); len(got) > 0 {
		t.Error(got)
	}
}

// The settings reach the client, and a settings request reaches the module, with every field.
func TestSettingsKeepEveryField(t *testing.T) {
	if got := copytest.Diff(copytest.Match{}, toSettings); len(got) > 0 {
		t.Errorf("Settings: %v", got)
	}
	if got := copytest.Diff(copytest.Match{}, settingsOf); len(got) > 0 {
		t.Errorf("UpdateSettings: %v", got)
	}
}
