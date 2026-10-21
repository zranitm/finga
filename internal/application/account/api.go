// Package account is the account module: sign up, log in, sessions with one
// active session per account (OD-08), and account settings. Its service
// implements pkg/auth, so handlers and the gateway depend on the interface
// (TD-17). It owns the accounts and sessions tables.
package account

import (
	"time"

	"finga/internal/model"
	"finga/pkg/errs"
)

// The module's errors. Handlers map the codes; the messages are for players.
var (
	ErrNotFound        = errs.New(errs.NotFound, "account not found")
	ErrUsernameTaken   = errs.New(errs.Conflict, "that username is taken")
	ErrUnauthenticated = errs.New(errs.Unauthenticated, "invalid credentials or session")
	ErrRateLimited     = errs.New(errs.RateLimited, "too many login attempts, try again in a minute")
	ErrBadUsername     = errs.Invalid("username must be 3 to 16 letters, digits or underscores")
	ErrBadPassword     = errs.Invalid("password must be 8 to 128 characters")
	ErrBadPreset       = errs.Invalid("control preset must be classic or wasd")

	// errActiveSession is the partial unique index speaking: the service
	// revokes first, so this only shows up in a race, and the login runs again.
	errActiveSession = errs.New(errs.Conflict, "another session is active")
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
	ID        model.AccountID
	Username  string
	Settings  Settings
	CreatedAt time.Time
}

// NewAccount is a row to insert.
type NewAccount struct {
	ID           model.AccountID
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
	ID        model.SessionID
	AccountID model.AccountID
	TokenHash []byte // SHA-256 of the token
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// RegisterInput is a sign-up.
type RegisterInput struct {
	Username string
	Password string
}

// LoginInput is a login. IP is the caller's address for throttling; empty
// skips the per-IP limit.
type LoginInput struct {
	Username string
	Password string
	IP       string
}
