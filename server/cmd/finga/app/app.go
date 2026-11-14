// Package app is the game's composition root: every module in one process, each calling the others' facades; shared with the end-to-end tests.
package app

import (
	"context"

	"finga/cmd/finga/admin"
	"finga/config"
	"finga/internal/host"
	"finga/modules/account"
	"finga/modules/character"
	gatewayapp "finga/modules/gateway/app"
	"finga/modules/item"
	"finga/modules/social"
	"finga/modules/world"
	"finga/pkg/uuidv7"
)

// Build adds every module to h from cfg, in the order they call each other, the gateway last, then the admin API if cfg enables it; it fails fast on bad game data or an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("account")
	if err != nil {
		return err
	}
	accounts, err := account.New(ctx, account.Deps{Module: m, Config: cfg.Account})
	if err != nil {
		return err
	}
	if m, err = h.Module("item"); err != nil {
		return err
	}
	items, err := item.New(ctx, item.Deps{Module: m, Config: cfg.Item, Data: data})
	if err != nil {
		return err
	}
	if m, err = h.Module("character"); err != nil {
		return err
	}
	characters, err := character.New(ctx, character.Deps{Module: m, Data: data, Item: items})
	if err != nil {
		return err
	}
	if m, err = h.Module("social"); err != nil {
		return err
	}
	parties, err := social.New(ctx, social.Deps{Module: m, Config: cfg.Social, Data: data})
	if err != nil {
		return err
	}
	if m, err = h.Module("world"); err != nil {
		return err
	}
	maps, err := world.New(ctx, world.Deps{Module: m, Config: cfg.World, Data: data, ID: uuidv7.NewString(), Character: characters, Item: items, Social: parties})
	if err != nil {
		return err
	}
	m.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	if err := gatewayapp.Build(ctx, h, cfg.Gateway, cfg.HTTP, gatewayapp.Modules{Account: accounts, Character: characters, Item: items, World: maps, Social: parties}); err != nil {
		return err
	}
	if !cfg.Admin.Enabled {
		return nil
	}
	if m, err = h.Module("admin"); err != nil {
		return err
	}
	h.Serve("admin", cfg.Admin.Addr, admin.Handler(admin.Deps{Log: m.Logger, Data: data, Accounts: accounts, Characters: characters, Items: items, World: maps}))
	return nil
}
