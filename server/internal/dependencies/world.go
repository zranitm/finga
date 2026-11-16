package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/application/social"
	"finga/internal/application/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
	"finga/pkg/uuidv7"
)

// buildWorld adds the world module to h, over the character, item and social modules.
func buildWorld(ctx context.Context, h *host.Host, cfg config.Config, data *gamedata.Data, characters *character.Module, items *item.Module, parties *social.Module) (*world.Module, error) {
	m, err := h.Module("world")
	if err != nil {
		return nil, err
	}
	maps, err := world.New(ctx, world.Deps{Module: m, Config: cfg.World, Data: data, ID: uuidv7.NewString(), Character: characters, Item: items, Social: parties})
	if err != nil {
		return nil, err
	}
	m.Logger.InfoContext(ctx, "built", "maps", len(data.Maps()), "classes", len(data.Classes()), "mobs", len(data.Mobs()))
	return maps, nil
}
