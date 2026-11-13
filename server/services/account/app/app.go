// Package app is the account service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/account"
	"finga/services/account/config"
	"finga/services/account/internal/handler"
)

// Build adds the account module to h from cfg and serves its facade over gRPC; it fails fast on an unreachable database.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	m, err := h.Module("account")
	if err != nil {
		return err
	}
	accounts, err := account.New(ctx, account.Deps{Module: m, Config: cfg})
	if err != nil {
		return err
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	accountv1.RegisterAccountServiceServer(srv, handler.New(accounts, uuidv7.NewString()))
	return nil
}
