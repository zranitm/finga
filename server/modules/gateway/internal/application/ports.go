package application

import (
	"context"

	"finga/internal/kernel"
	"finga/modules/gateway/internal/model"
)

// Ports fail with a model.ServiceError.

// Accounts is the account service, for the REST API.
type Accounts interface {
	Register(ctx context.Context, username, password string) (model.Account, error)
	Login(ctx context.Context, username, password string) (string, error)
	Settings(ctx context.Context, account kernel.AccountID) (model.Settings, error)
	UpdateSettings(ctx context.Context, account kernel.AccountID, s model.Settings) error
}

// Sessions checks and ends login sessions, for the REST API.
type Sessions interface {
	// Validate resolves a token to its login session; unknown, expired and revoked tokens fail alike.
	Validate(ctx context.Context, token string) (model.Principal, error)
	// Revoke ends one session; revoking an ended session is not an error.
	Revoke(ctx context.Context, session kernel.SessionID) error
	RevokeAll(ctx context.Context, account kernel.AccountID) error
}

// Characters is the character service, for the REST API.
type Characters interface {
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	Create(ctx context.Context, account kernel.AccountID, name string) (model.Character, error)
	SetSkillBar(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string) error
}
