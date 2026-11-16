package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/social"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildSocial adds the social module to h.
func buildSocial(ctx context.Context, h *host.Host, cfg config.Config, data *gamedata.Data) (*social.Module, error) {
	m, err := h.Module("social")
	if err != nil {
		return nil, err
	}
	return social.New(ctx, social.Deps{Module: m, Config: cfg.Social, Data: data})
}
