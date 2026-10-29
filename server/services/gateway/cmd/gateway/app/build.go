package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/telemetry"
	"finga/pkg/grpcx"
	"finga/pkg/observability"
	"finga/services/gateway/config"
	"finga/services/gateway/internal/clients"
	"finga/services/gateway/internal/handler/api"
	"finga/services/gateway/internal/handler/ws"
)

// BuildOptions is everything Build needs.
type BuildOptions struct {
	Config   config.Config
	Logger   *slog.Logger
	Registry *prometheus.Registry
}

// Build wires the gateway as production runs it: a client for each
// service (page 10), the REST handler and the gateway, metrics, and the
// server. It needs no database and no game data: every table belongs to a
// service, and the world runs the maps. A service it dials is reached
// lazily, and a call fails while it is down.
func Build(ctx context.Context, opts BuildOptions) (*Server, error) {
	cfg := opts.Config
	metrics, err := telemetry.NewMetrics(opts.Registry)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	conns, err := dialAll(cfg.Dependencies)
	if err != nil {
		return nil, err
	}
	accounts := accountv1.NewAccountServiceClient(conns.account)
	characters := characterv1.NewCharacterServiceClient(conns.character)

	restAPI := api.NewHandler(api.Dependencies{
		Accounts:    accounts,
		Characters:  characters,
		Sessions:    clients.NewGuard(accounts),
		Logger:      opts.Logger,
		CORSOrigins: cfg.HTTP.CORSOrigins,
	})
	gateway := ws.New(ws.Dependencies{
		ID:          uuid.NewString(),
		Sessions:    clients.NewGuard(accounts),
		Watcher:     accounts,
		World:       worldv1.NewWorldServiceClient(conns.world),
		Items:       itemv1.NewItemServiceClient(conns.item),
		Social:      socialv1.NewSocialServiceClient(conns.social),
		Logger:      opts.Logger,
		Observer:    metrics,
		CORSOrigins: cfg.HTTP.CORSOrigins,
	})
	s := New(Options{HTTP: cfg.HTTP, ShutdownGrace: cfg.ShutdownGrace, Logger: opts.Logger}, Deps{
		API:     restAPI,
		Gateway: gateway,
		Metrics: observability.MetricsHandler(opts.Registry),
		Tasks:   []Task{{Name: "streams", Run: gateway.Run}},
		Drain:   gateway.Drain,
	})
	s.closers = append(s.closers, conns.close)
	opts.Logger.InfoContext(ctx, "built")
	return s, nil
}

// conns are the gateway's connections to the services.
type conns struct {
	account, character, item, world, social *grpc.ClientConn
}

// dialAll makes a connection to each service; nothing connects until the
// first call.
func dialAll(s config.Dependencies) (conns, error) {
	var c conns
	for _, d := range []struct {
		name string
		addr string
		conn **grpc.ClientConn
	}{
		{"account", s.Account, &c.account},
		{"character", s.Character, &c.character},
		{"item", s.Item, &c.item},
		{"world", s.World, &c.world},
		{"social", s.Social, &c.social},
	} {
		conn, err := grpcx.Dial(d.addr)
		if err != nil {
			c.close()
			return conns{}, fmt.Errorf("%s service: %w", d.name, err)
		}
		*d.conn = conn
	}
	return c, nil
}

func (c conns) close() {
	for _, conn := range []*grpc.ClientConn{c.account, c.character, c.item, c.world, c.social} {
		if conn != nil {
			_ = conn.Close()
		}
	}
}
