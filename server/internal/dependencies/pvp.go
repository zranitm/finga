package dependencies

import (
	"context"

	"finga/internal/application/character"
	"finga/internal/application/item"
	"finga/internal/application/pvp"
	"finga/internal/application/social"
	"finga/internal/application/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildPvp adds the pvp module to h, over the world, social, character and item modules.
func buildPvp(ctx context.Context, h *host.Host, data *gamedata.Data, maps *world.Module, parties *social.Module, characters *character.Module, items *item.Module) (*pvp.Module, error) {
	m, err := h.Module("pvp")
	if err != nil {
		return nil, err
	}
	return pvp.New(ctx, pvp.Deps{Module: m, Data: data, World: maps, Social: parties, Character: characters, Items: items})
}
