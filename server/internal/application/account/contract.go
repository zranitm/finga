package account

import (
	"maps"
	"time"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
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
	AutoAssignPoints bool    // the client spends each level's points by the class's split
	ChatPinned       bool    // the chat stays open while the player isn't typing
	HideMinimap      bool    // the minimap is hidden
	Zoom             float64 // how far the camera has pulled back, 0 close to 1 far
	// Keys are the key bindings that differ from the client's defaults, an action to a KeyboardEvent.code.
	Keys map[string]string
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
	return Settings{AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned, HideMinimap: s.HideMinimap, Zoom: s.Zoom, Keys: maps.Clone(s.Keys)}
}

func (s Settings) model() model.Settings {
	return model.Settings{AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned, HideMinimap: s.HideMinimap, Zoom: s.Zoom, Keys: maps.Clone(s.Keys)}
}
