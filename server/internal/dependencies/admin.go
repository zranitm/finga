package dependencies

import (
	"finga/config"
	"finga/internal/handler/admin"
	"finga/internal/shared/host"
)

// buildAdmin serves the admin API on h, over the modules in deps, if cfg enables it.
func buildAdmin(h *host.Host, cfg config.Config, deps admin.Deps) error {
	if !cfg.Admin.Enabled {
		return nil
	}
	m, err := h.Module("admin")
	if err != nil {
		return err
	}
	deps.Log = m.Logger
	h.Serve("admin", cfg.Admin.Addr, admin.Handler(deps))
	return nil
}
