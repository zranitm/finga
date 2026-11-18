// Package account holds the account module's accounts, sessions, settings and errors.
package account

import (
	"errors"
	"regexp"
	"slices"
	"time"

	"finga/internal/shared/kernel"
)

// The module's errors; the handler sends each as a contract reason.
var (
	ErrNotFound        = errors.New("account: not found")
	ErrUsernameTaken   = errors.New("account: username taken")
	ErrUnauthenticated = errors.New("account: invalid credentials or session")
	ErrRateLimited     = errors.New("account: too many login attempts")
	ErrBadUsername     = errors.New("account: bad username")
	ErrBadPassword     = errors.New("account: bad password")
	ErrBadKeyBindings  = errors.New("account: bad key bindings")
	ErrBadZoom         = errors.New("account: bad zoom")

	// ErrSessionRace means two logins raced; it escapes only once the login retries run out.
	ErrSessionRace = errors.New("account: another session became active during login")
	ErrBusy        = errors.New("account: too many password checks at once")
)

// Settings are the account's settings, stored as JSON.
type Settings struct {
	AutoAssignPoints bool    `json:"auto_assign_points"` // the client spends each level's points by the class's split
	ChatPinned       bool    `json:"chat_pinned"`        // the chat stays open while the player isn't typing
	HideMinimap      bool    `json:"hide_minimap"`       // the minimap is hidden; stored as hidden so an account saved before it shows it
	Zoom             float64 `json:"zoom"`               // how far the camera has pulled back, 0 close to 1 far
	// Keys are the player's key bindings that differ from the client's defaults, an action to a KeyboardEvent.code; the actions are the client's.
	Keys map[string]string `json:"keys,omitempty"`
}

// MaxKeyBindings is how many changed keys an account keeps, well over the client's actions.
const MaxKeyBindings = 64

var keyWord = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// reservedKeys no action takes: Enter and Esc open the chat and close windows, and the system and the browser keep the rest.
var reservedKeys = []string{"Escape", "Enter", "NumpadEnter", "MetaLeft", "MetaRight", "ContextMenu", "F5", "F11", "F12"}

// Check checks the zoom is from 0 to 1, and the key bindings' shape, not their actions: at most MaxKeyBindings, each action and key a word, no key twice, and no reserved key.
func (s Settings) Check() error {
	if s.Zoom < 0 || s.Zoom > 1 {
		return ErrBadZoom
	}
	if len(s.Keys) > MaxKeyBindings {
		return ErrBadKeyBindings
	}
	taken := make(map[string]bool, len(s.Keys))
	for action, code := range s.Keys {
		if !keyWord.MatchString(action) || !keyWord.MatchString(code) || slices.Contains(reservedKeys, code) || taken[code] {
			return ErrBadKeyBindings
		}
		taken[code] = true
	}
	return nil
}

// Account is what callers see of an account: never the password hash.
type Account struct {
	ID        kernel.AccountID
	Username  string
	Settings  Settings
	CreatedAt time.Time
}

// NewAccount is a row to insert.
type NewAccount struct {
	ID           kernel.AccountID
	Username     string
	PasswordHash string
	Settings     Settings
}

// Credentials are an account with its password hash, for verification.
type Credentials struct {
	Account      Account
	PasswordHash string
}

// Principal is who a validated session belongs to.
type Principal struct {
	AccountID kernel.AccountID
	SessionID kernel.SessionID
}

// Session is a login session. The token itself is never stored.
type Session struct {
	ID        kernel.SessionID
	AccountID kernel.AccountID
	TokenHash []byte // SHA-256 of the token
	// PreviousTokenHash is the token retired at RotatedAt; RotatedAt clears once it is used.
	PreviousTokenHash []byte
	RotatedAt         *time.Time
	CreatedAt         time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
}
