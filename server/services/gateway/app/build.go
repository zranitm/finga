package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/grpcx"
	"finga/pkg/uuidv7"
	"finga/services/gateway/config"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/application/push"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/clients"
	"finga/services/gateway/internal/handler"
	"finga/services/gateway/internal/handler/api"
	"finga/services/gateway/internal/handler/ws"
)

// The players' listener's name, as the logs show it.
const httpListener = "http"

// Build wires the gateway as production runs it; services are dialed lazily, so a call fails while one is down.
func Build(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	h, err := host.New(host.Options{Name: "gateway", Settings: cfg.Host(), Logger: log, Registry: reg})
	if err != nil {
		return nil, err
	}
	metrics, err := telemetry.NewGatewayMetrics(reg)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	rec := h.Panics
	conns, err := dialAll(cfg.Dependencies)
	if err != nil {
		return nil, err
	}
	h.OnClose(conns.close)
	accounts := accountv1.NewAccountServiceClient(conns.account)
	guard := clients.NewGuard(accounts)

	lobby := application.NewLobby(clients.NewAccounts(accounts), clients.NewCharacters(characterv1.NewCharacterServiceClient(conns.character)), guard)
	world := clients.NewWorld(worldv1.NewWorldServiceClient(conns.world), log, rec)
	social := clients.NewSocial(socialv1.NewSocialServiceClient(conns.social), log, rec)
	sessions := session.NewRegistry(session.Deps{Gateway: uuidv7.NewString(), World: world, Social: social, Logger: log, Panics: rec, JoinTimeout: cfg.Sessions.JoinTimeout})
	sendFlow := send.New(send.Deps{
		Sessions:    guard,
		World:       world,
		Items:       clients.NewItems(itemv1.NewItemServiceClient(conns.item)),
		Social:      social,
		Logger:      log,
		Observer:    metrics,
		JoinTimeout: cfg.Sessions.JoinTimeout,
	}, sessions)
	pushFlow := push.New(push.Deps{
		Sessions:             guard,
		World:                world,
		Social:               social,
		Account:              clients.NewSessionEnds(accounts, log, rec),
		Logger:               log,
		SessionCheckInterval: cfg.Sessions.CheckInterval,
	}, sessions)
	h.Serve(httpListener, cfg.HTTP.Addr, handler.NewRouter(handler.Routes{
		API: api.Mount(api.Dependencies{Lobby: lobby, Logger: log, CORSOrigins: cfg.HTTP.CORSOrigins, Panics: rec}),
		Gateway: ws.New(sendFlow, ws.Options{
			Origins: cfg.HTTP.CORSOrigins, HelloTimeout: cfg.WS.HelloTimeout, CommandQueue: cfg.WS.CommandQueue,
			Observer: metrics, Logger: log, Panics: rec,
		}),
	}))
	h.Task("streams", pushFlow.Run)
	h.OnDrain(pushFlow.Drain)
	log.InfoContext(ctx, "built")
	return h, nil
}

type conns struct {
	account, character, item, world, social *grpc.ClientConn
}

// dialAll makes a connection to each service; nothing connects until the first call.
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
