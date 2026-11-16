// Package dependencies is the game's composition root, the one package that imports every layer of every module: it builds each module in one process and connects each to the others' facades, a file per area; shared with the end-to-end tests.
package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/handler/admin"
	"finga/internal/shared/host"
)

// Build adds every module to h from cfg, in the order they call each other, the gateway last, then the admin API if cfg enables it; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	accounts, err := buildAccount(ctx, h, cfg)
	if err != nil {
		return err
	}
	items, err := buildItem(ctx, h, cfg, data)
	if err != nil {
		return err
	}
	characters, err := buildCharacter(ctx, h, data, items)
	if err != nil {
		return err
	}
	parties, err := buildSocial(ctx, h, cfg, data)
	if err != nil {
		return err
	}
	maps, err := buildWorld(ctx, h, cfg, data, characters, items, parties)
	if err != nil {
		return err
	}
	if err := buildGateway(ctx, h, cfg.Gateway, cfg.HTTP, gatewayModules{Account: accounts, Character: characters, Item: items, World: maps, Social: parties}); err != nil {
		return err
	}
	return buildAdmin(h, cfg, admin.Deps{Data: data, Accounts: accounts, Characters: characters, Items: items, World: maps})
}
