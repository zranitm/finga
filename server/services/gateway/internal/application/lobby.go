package application

import (
	"context"

	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/services/gateway/internal/model"
)

// Lobby is the REST API's use cases, before a player enters the world.
type Lobby struct {
	accounts   Accounts
	characters Characters
	sessions   auth.Guard
}

// NewLobby builds the use cases over the account and character services.
func NewLobby(accounts Accounts, characters Characters, sessions auth.Guard) *Lobby {
	return &Lobby{accounts: accounts, characters: characters, sessions: sessions}
}

// Authenticate resolves a bearer token to its login session.
func (l *Lobby) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
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
func (l *Lobby) Logout(ctx context.Context, p auth.Principal) error {
	return l.sessions.Revoke(ctx, p.SessionID)
}

// LogoutEverywhere ends every session of the caller's account, closing its connections.
func (l *Lobby) LogoutEverywhere(ctx context.Context, p auth.Principal) error {
	return l.sessions.RevokeAll(ctx, p.AccountID)
}

// Characters lists the caller's characters.
func (l *Lobby) Characters(ctx context.Context, p auth.Principal) ([]model.Character, error) {
	return l.characters.List(ctx, kernel.AccountID(p.AccountID))
}

// CreateCharacter creates a character for the caller.
func (l *Lobby) CreateCharacter(ctx context.Context, p auth.Principal, name, class string) (model.Character, error) {
	return l.characters.Create(ctx, kernel.AccountID(p.AccountID), name, class)
}

// Settings are the caller's account settings.
func (l *Lobby) Settings(ctx context.Context, p auth.Principal) (model.Settings, error) {
	return l.accounts.Settings(ctx, kernel.AccountID(p.AccountID))
}

// UpdateSettings replaces the caller's account settings.
func (l *Lobby) UpdateSettings(ctx context.Context, p auth.Principal, s model.Settings) error {
	return l.accounts.UpdateSettings(ctx, kernel.AccountID(p.AccountID), s)
}
