package application

import (
	"context"

	"finga/internal/kernel"
	"finga/modules/gateway/internal/model"
)

// Lobby is the REST API's use cases, before a player enters the world.
type Lobby struct {
	accounts   Accounts
	characters Characters
	sessions   Sessions
}

// LobbyDeps are what the lobby is built over: the account and character modules and the session guard.
type LobbyDeps struct {
	Accounts   Accounts
	Characters Characters
	Sessions   Sessions
}

// NewLobby builds the use cases over deps.
func NewLobby(deps LobbyDeps) *Lobby {
	return &Lobby{accounts: deps.Accounts, characters: deps.Characters, sessions: deps.Sessions}
}

// Authenticate resolves a bearer token to its login session.
func (l *Lobby) Authenticate(ctx context.Context, token string) (model.Principal, error) {
	return l.sessions.Validate(ctx, token)
}

// Register signs up.
func (l *Lobby) Register(ctx context.Context, username, password string) (model.Account, error) {
	return l.accounts.Register(ctx, username, password)
}

// Login returns a new session token; it is throttled per username here and per IP at the edge.
func (l *Lobby) Login(ctx context.Context, username, password string) (string, error) {
	return l.accounts.Login(ctx, username, password)
}

// Logout ends the caller's login session.
func (l *Lobby) Logout(ctx context.Context, p model.Principal) error {
	return l.sessions.Revoke(ctx, p.SessionID)
}

// LogoutEverywhere ends every session of the caller's account, closing its connections.
func (l *Lobby) LogoutEverywhere(ctx context.Context, p model.Principal) error {
	return l.sessions.RevokeAll(ctx, p.AccountID)
}

// Characters lists the caller's characters.
func (l *Lobby) Characters(ctx context.Context, p model.Principal) ([]model.Character, error) {
	return l.characters.List(ctx, p.AccountID)
}

// CreateCharacter creates a character for the caller, a Beginner.
func (l *Lobby) CreateCharacter(ctx context.Context, p model.Principal, name string) (model.Character, error) {
	return l.characters.Create(ctx, p.AccountID, name)
}

// SetSkillBar stores which skill sits on each key of one of the caller's characters.
func (l *Lobby) SetSkillBar(ctx context.Context, p model.Principal, id kernel.CharacterID, bar []string) error {
	return l.characters.SetSkillBar(ctx, p.AccountID, id, bar)
}

// Settings are the caller's account settings.
func (l *Lobby) Settings(ctx context.Context, p model.Principal) (model.Settings, error) {
	return l.accounts.Settings(ctx, p.AccountID)
}

// UpdateSettings replaces the caller's account settings.
func (l *Lobby) UpdateSettings(ctx context.Context, p model.Principal, s model.Settings) error {
	return l.accounts.UpdateSettings(ctx, p.AccountID, s)
}
