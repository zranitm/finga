// Package app is the social service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/social"
	"finga/services/social/config"
	"finga/services/social/internal/handler"
)

// Build adds the social module to h from cfg and serves its facade over gRPC; parties live in memory, so a restart forgets them.
func Build(ctx context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("social")
	if err != nil {
		return err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	parties, err := social.New(ctx, social.Deps{Module: m, Config: cfg, Data: data, Notifier: streams})
	if err != nil {
		return err
	}
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	socialv1.RegisterSocialServiceServer(srv, handler.New(parties, streams, m.Panics))
	return nil
}
