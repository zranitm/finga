package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/account"
	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/application/world"
	handlercharacter "finga/internal/handler/character"
	"finga/internal/handler/transport"
	handlerworld "finga/internal/handler/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
	"finga/internal/shared/kernel"
)

// buildAdmin serves the admin API on h, over the modules, if cfg enables it: each module's handler adds its routes.
func buildAdmin(h *host.Host, cfg config.Config, data *gamedata.Data, accounts *account.Module, characters *character.Module, items *item.Module, maps *world.Module) error {
	if !cfg.Admin.Enabled {
		return nil
	}
	m, err := h.Module("admin")
	if err != nil {
		return err
	}
	admin := transport.NewAdmin(transport.AdminOptions{
		Validate: func(ctx context.Context, token string) (kernel.AccountID, error) {
			p, err := accounts.Validate(ctx, token)
			return p.AccountID, err
		},
		Log: m.Logger,
	},
		handlercharacter.AdminRoutes(handlercharacter.AdminDeps{Characters: characters, Items: items}),
		handlerworld.AdminRoutes(handlerworld.AdminDeps{Data: data, Characters: characters, World: maps}),
	)
	h.Serve("admin", cfg.Admin.Addr, admin)
	return nil
}
