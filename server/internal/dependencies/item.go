package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/item"
	repository "finga/internal/repository/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildItem adds the item module to h.
func buildItem(ctx context.Context, h *host.Host, cfg config.Config, data *gamedata.Data) (*item.Module, error) {
	m, err := h.Module("item")
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := m.Database(ctx) // fails fast on an unreachable database
	if err != nil {
		return nil, err
	}
	return item.New(item.Deps{Module: m, Config: cfg.Item, Data: data, Pool: pool, Store: repository.NewPostgres(pool, dbMetrics)})
}
