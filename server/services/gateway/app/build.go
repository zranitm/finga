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
	"finga/services/gateway/internal/application"
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

// Build wires the gateway as production runs it (page 11): an adapter for
// each service it calls (page 10), the use cases over them (the REST API's
// Lobby, the connections' Gateway), the REST and WebSocket handlers,
// metrics, and the server. It needs no database and no game data: every table belongs to a
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
	guard := clients.NewGuard(accounts)

	lobby := application.NewLobby(clients.NewAccounts(accounts), clients.NewCharacters(characterv1.NewCharacterServiceClient(conns.character)), guard)
	gateway := application.NewGateway(application.Dependencies{
		ID:          uuid.NewString(),
		Sessions:    guard,
		SessionEnds: clients.NewSessionEnds(accounts, opts.Logger),
		World:       clients.NewWorld(worldv1.NewWorldServiceClient(conns.world), opts.Logger),
		Items:       clients.NewItems(itemv1.NewItemServiceClient(conns.item)),
		Social:      clients.NewSocial(socialv1.NewSocialServiceClient(conns.social), opts.Logger),
		Logger:      opts.Logger,
		Observer:    metrics,
	})
	s := New(Options{HTTP: cfg.HTTP, ShutdownGrace: cfg.ShutdownGrace, Logger: opts.Logger}, Deps{
		API:     api.NewHandler(api.Dependencies{Lobby: lobby, Logger: opts.Logger, CORSOrigins: cfg.HTTP.CORSOrigins}),
		Gateway: ws.New(gateway, cfg.HTTP.CORSOrigins),
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
