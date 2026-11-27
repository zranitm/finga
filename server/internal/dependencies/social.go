package dependencies

import (
	"finga/config"
	"finga/internal/application/social"
	repository "finga/internal/repository/social"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/host"
)

// buildSocial adds the social module to h.
func buildSocial(h *host.Host, cfg config.Config, data *gamedata.Data) (*social.Module, error) {
	m, err := h.Module("social")
	if err != nil {
		return nil, err
	}
	return social.New(social.Deps{Module: m, Config: cfg.Social, Data: data, Store: repository.NewMemory()})
}
