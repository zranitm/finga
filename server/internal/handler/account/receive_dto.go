package account

import (
	"time"

	model "finga/internal/model/account"
)

// RegisterRequest is POST /api/accounts.
type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AccountResponse is an account, never with its password hash.
type AccountResponse struct { //nolint:revive // named as its schema in api/openapi.yaml, which the drift test holds it to
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// LoginRequest is POST /api/sessions.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SessionResponse carries the opaque token, the only time it is visible.
type SessionResponse struct {
	Token string `json:"token"`
}

// SettingsRequest is PUT /api/settings; a flag left out is false, the zoom 0, and keys left out are the defaults.
type SettingsRequest struct {
	AutoAssignPoints bool              `json:"auto_assign_points"`
	ChatPinned       bool              `json:"chat_pinned"`
	HideMinimap      bool              `json:"hide_minimap"`
	Zoom             float64           `json:"zoom"`
	Walk             bool              `json:"walk"`
	Keys             map[string]string `json:"keys"`
}

// SettingsResponse is the account's settings.
type SettingsResponse struct {
	AutoAssignPoints bool              `json:"auto_assign_points"`
	ChatPinned       bool              `json:"chat_pinned"`
	HideMinimap      bool              `json:"hide_minimap"`
	Zoom             float64           `json:"zoom"`
	Walk             bool              `json:"walk"`
	Keys             map[string]string `json:"keys"` // never null: {} for the defaults
}

func toAccount(a model.Summary) AccountResponse {
	return AccountResponse{ID: a.ID.String(), Username: a.Username, CreatedAt: a.CreatedAt}
}

func toSettings(s model.Settings) SettingsResponse {
	keys := s.Keys
	if keys == nil {
		keys = map[string]string{}
	}
	return SettingsResponse{AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned, HideMinimap: s.HideMinimap, Zoom: s.Zoom, Walk: s.Walk, Keys: keys}
}
