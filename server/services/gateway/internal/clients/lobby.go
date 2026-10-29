package clients

import (
	"context"

	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	"finga/internal/kernel"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// AccountService is what the REST API needs of the account service's
// generated client.
type AccountService interface {
	Register(ctx context.Context, in *accountv1.RegisterRequest, opts ...grpc.CallOption) (*accountv1.RegisterResponse, error)
	Login(ctx context.Context, in *accountv1.LoginRequest, opts ...grpc.CallOption) (*accountv1.LoginResponse, error)
	GetSettings(ctx context.Context, in *accountv1.GetSettingsRequest, opts ...grpc.CallOption) (*accountv1.GetSettingsResponse, error)
	UpdateSettings(ctx context.Context, in *accountv1.UpdateSettingsRequest, opts ...grpc.CallOption) (*accountv1.UpdateSettingsResponse, error)
}

// Accounts is the application's Accounts over the account service.
type Accounts struct{ c AccountService }

// NewAccounts adapts the account client.
func NewAccounts(c AccountService) Accounts { return Accounts{c: c} }

// Register signs up.
func (a Accounts) Register(ctx context.Context, username, password string) (model.Account, error) {
	res, err := a.c.Register(ctx, &accountv1.RegisterRequest{Username: username, Password: password})
	if err != nil {
		return model.Account{}, errmap.Wrap(err)
	}
	acc := res.GetAccount()
	id, err := kernel.ParseAccountID(acc.GetId())
	if err != nil {
		return model.Account{}, errmap.Wrap(err)
	}
	return model.Account{ID: id, Username: acc.GetUsername(), CreatedAt: acc.GetCreatedAt().AsTime()}, nil
}

// Login opens a session.
func (a Accounts) Login(ctx context.Context, username, password, ip string) (string, error) {
	res, err := a.c.Login(ctx, &accountv1.LoginRequest{Username: username, Password: password, Ip: ip})
	return res.GetToken(), errmap.Wrap(err)
}

// Settings are the account's settings.
func (a Accounts) Settings(ctx context.Context, account kernel.AccountID) (model.Settings, error) {
	res, err := a.c.GetSettings(ctx, &accountv1.GetSettingsRequest{AccountId: account.String()})
	if err != nil {
		return model.Settings{}, errmap.Wrap(err)
	}
	return model.Settings{ControlPreset: res.GetSettings().GetControlPreset()}, nil
}

// UpdateSettings replaces the account's settings.
func (a Accounts) UpdateSettings(ctx context.Context, account kernel.AccountID, s model.Settings) error {
	_, err := a.c.UpdateSettings(ctx, &accountv1.UpdateSettingsRequest{AccountId: account.String(), Settings: &accountv1.Settings{ControlPreset: s.ControlPreset}})
	return errmap.Wrap(err)
}

// CharacterService is what the REST API needs of the character service's
// generated client.
type CharacterService interface {
	Create(ctx context.Context, in *characterv1.CreateRequest, opts ...grpc.CallOption) (*characterv1.CreateResponse, error)
	List(ctx context.Context, in *characterv1.ListRequest, opts ...grpc.CallOption) (*characterv1.ListResponse, error)
}

// Characters is the application's Characters over the character service.
type Characters struct{ c CharacterService }

// NewCharacters adapts the character client.
func NewCharacters(c CharacterService) Characters { return Characters{c: c} }

// List lists the account's characters.
func (c Characters) List(ctx context.Context, account kernel.AccountID) ([]model.Character, error) {
	res, err := c.c.List(ctx, &characterv1.ListRequest{AccountId: account.String()})
	if err != nil {
		return nil, errmap.Wrap(err)
	}
	out := make([]model.Character, 0, len(res.GetCharacters()))
	for _, ch := range res.GetCharacters() {
		m, err := character(ch)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Create creates a character.
func (c Characters) Create(ctx context.Context, account kernel.AccountID, name, class string) (model.Character, error) {
	res, err := c.c.Create(ctx, &characterv1.CreateRequest{AccountId: account.String(), Name: name, Class: class})
	if err != nil {
		return model.Character{}, errmap.Wrap(err)
	}
	return character(res.GetCharacter())
}

func character(c *characterv1.Character) (model.Character, error) {
	id, err := kernel.ParseCharacterID(c.GetId())
	if err != nil {
		return model.Character{}, errmap.Wrap(err)
	}
	s := c.GetStats()
	return model.Character{
		ID: id, Name: c.GetName(), Class: c.GetClass(), Level: int(c.GetLevel()), Exp: c.GetExp(),
		Stats: model.Stats{STR: int(s.GetStrength()), DEX: int(s.GetDexterity()), INT: int(s.GetIntelligence()), AGI: int(s.GetAgility()), VIT: int(s.GetVitality())},
		HP:    int(c.GetHp()), MP: int(c.GetMp()), MapID: c.GetMapId(),
	}, nil
}
