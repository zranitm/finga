// Package model is the account service's model (Low-level design, page
// 11): accounts, sessions (one active per account, OD-08), settings, and
// the errors its use cases return. It needs nothing but data.
package model

import (
	"errors"
	"time"

	"finga/internal/kernel"
)

// The service's errors. Each names one failure; the service's handler sends
// it as a reason, and the gateway gives it a code, a status and the words a
// player sees (its errmap).
var (
	ErrNotFound        = errors.New("account: not found")
	ErrUsernameTaken   = errors.New("account: username taken")
	ErrUnauthenticated = errors.New("account: invalid credentials or session")
	ErrRateLimited     = errors.New("account: too many login attempts")
	ErrBadUsername     = errors.New("account: bad username")
	ErrBadPassword     = errors.New("account: bad password")
	ErrBadPreset       = errors.New("account: bad control preset")

	// ErrSessionRace is the partial unique index speaking: the service
	// revokes first, so this only shows up when two logins race, and the
	// login runs again; it leaves the service only when the retries run out.
	ErrSessionRace = errors.New("account: another session became active during login")
)

// Preset is a control preset (B6, OD-20).
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
	CreatedAt    time.Time
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
	// PreviousTokenHash is the token retired by the last rotation; RotatedAt is
	// when, cleared once that token has been used (step 16).
	PreviousTokenHash []byte
	RotatedAt         *time.Time
	CreatedAt         time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
}
