package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildItem adds the item module to h.
func buildItem(ctx context.Context, h *host.Host, cfg config.Config, data *gamedata.Data) (*item.Module, error) {
	m, err := h.Module("item")
	if err != nil {
		return nil, err
	}
	return item.New(ctx, item.Deps{Module: m, Config: cfg.Item, Data: data})
}
