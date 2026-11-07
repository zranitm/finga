// Package model holds the account service's accounts, sessions, settings and errors.
package model

import (
	"errors"
	"time"

	"finga/internal/kernel"
)

// The service's errors; the handler sends each as a contract reason.
var (
	ErrNotFound        = errors.New("account: not found")
	ErrUsernameTaken   = errors.New("account: username taken")
	ErrUnauthenticated = errors.New("account: invalid credentials or session")
	ErrRateLimited     = errors.New("account: too many login attempts")
	ErrBadUsername     = errors.New("account: bad username")
	ErrBadPassword     = errors.New("account: bad password")
	ErrBadPreset       = errors.New("account: bad control preset")

	// ErrSessionRace means two logins raced; it escapes only once the login retries run out.
	ErrSessionRace = errors.New("account: another session became active during login")
	ErrBusy        = errors.New("account: too many password checks at once")
)

// Preset is a control preset.
type Preset string

// The two presets.
const (
	PresetClassic Preset = "classic" // arrows
	PresetWASD    Preset = "wasd"
)

// Settings are the account's settings, stored as JSON.
type Settings struct {
	ControlPreset Preset `json:"control_preset"`
}

// DefaultSettings are a new account's settings.
func DefaultSettings() Settings { return Settings{ControlPreset: PresetClassic} }

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
