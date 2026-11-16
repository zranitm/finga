package dependencies

import (
	"context"

	"finga/config"
	"finga/internal/application/account"
	"finga/internal/shared/host"
)

// buildAccount adds the account module to h.
func buildAccount(ctx context.Context, h *host.Host, cfg config.Config) (*account.Module, error) {
	m, err := h.Module("account")
	if err != nil {
		return nil, err
	}
	return account.New(ctx, account.Deps{Module: m, Config: cfg.Account})
}
