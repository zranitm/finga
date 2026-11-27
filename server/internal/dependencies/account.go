package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/account"
	repository "finga/internal/repository/account"
	"finga/internal/shared/host"
)

// buildAccount adds the account module to h.
func buildAccount(ctx context.Context, h *host.Host, cfg config.Config) (*account.Module, error) {
	m, err := h.Module("account")
	if err != nil {
		return nil, err
	}
	pool, dbMetrics, err := m.Database(ctx) // fails fast on an unreachable database
	if err != nil {
		return nil, err
	}
	return account.New(account.Deps{Module: m, Config: cfg.Account, Store: repository.NewPostgres(pool, dbMetrics)})
}
