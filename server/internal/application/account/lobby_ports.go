package account

import (
	"context"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// Ports fail with a failure.ServiceError.

// LobbyAccounts is the account module, for the REST API.
type LobbyAccounts interface {
	Register(ctx context.Context, username, password string) (model.Summary, error)
	Login(ctx context.Context, username, password string) (string, error)
	Settings(ctx context.Context, account kernel.AccountID) (model.Settings, error)
	UpdateSettings(ctx context.Context, account kernel.AccountID, s model.Settings) error
}

// LobbySessions checks and ends login sessions, for the REST API.
type LobbySessions interface {
	// Validate resolves a token to its login session; unknown, expired and revoked tokens fail alike.
	Validate(ctx context.Context, token string) (model.Principal, error)
	// Revoke ends one session; revoking an ended session is not an error.
	Revoke(ctx context.Context, session kernel.SessionID) error
	RevokeAll(ctx context.Context, account kernel.AccountID) error
}

// LobbyWardrobes is the item module's wardrobe, for the REST API.
type LobbyWardrobes interface {
	Pieces(ctx context.Context, account kernel.AccountID) ([]model.Piece, error)
}

// LobbyArena is the pvp module's profiles, arena builds and boards, for the REST API.
type LobbyArena interface {
	Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (model.PvpProfile, error)
	SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, b model.ArenaBuild) error
	Leaderboard(ctx context.Context, mode string) ([]model.PvpRow, error)
}

// LobbyCharacters is the character module, for the REST API.
type LobbyCharacters interface {
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	Create(ctx context.Context, account kernel.AccountID, name string) (model.Character, error)
	SetSkillBar(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string) error
}
