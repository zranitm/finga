package account

import (
	"time"

	"finga/internal/kernel"
	"finga/services/account/internal/model"
)

// Credentials are a username and a password, to register or to log in.
type Credentials struct {
	Username string
	Password string
}

// Account is an account as its callers see it.
type Account struct {
	ID        kernel.AccountID
	Username  string
	Settings  Settings
	CreatedAt time.Time
}

// Settings are an account's game settings.
type Settings struct {
	ControlPreset    string // classic (arrows) or wasd
	AutoAssignPoints bool   // the client spends each level's points by the class's split
	ChatPinned       bool   // the chat stays open while the player isn't typing
}

// Principal is who a session's token speaks for.
type Principal struct {
	AccountID kernel.AccountID
	SessionID kernel.SessionID
}

func accountOf(a model.Account) Account {
	return Account{ID: a.ID, Username: a.Username, Settings: settingsOf(a.Settings), CreatedAt: a.CreatedAt}
}

func settingsOf(s model.Settings) Settings {
	return Settings{ControlPreset: string(s.ControlPreset), AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned}
}

func (s Settings) model() model.Settings {
	return model.Settings{ControlPreset: model.Preset(s.ControlPreset), AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned}
}
