// Package app is the gateway's composition root, over gRPC for its own binary and in the process for cmd/finga.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/host"
	"finga/pkg/panics"
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
	"finga/services/gateway/internal/handler/ws/receive"
	"finga/services/gateway/internal/local"
	"finga/services/gateway/internal/metrics"
)

// The players' listener's name, as the logs show it.
const httpListener = "http"

// Build adds the gateway module to h from cfg, serving players at public; services are dialed lazily, so a call fails while one is down.
func Build(ctx context.Context, h *host.Host, cfg config.Config, public host.HTTPSection) error {
	return build(ctx, h, cfg, public, func(log *slog.Logger, rec *panics.Recorder) (ports, error) {
		conns, err := dialAll(h, cfg.Dependencies)
		if err != nil {
			return ports{}, err
		}
		accounts := accountv1.NewAccountServiceClient(conns.account)
		return ports{
			accounts:    clients.NewAccounts(accounts),
			characters:  clients.NewCharacters(characterv1.NewCharacterServiceClient(conns.character)),
			guard:       clients.NewGuard(accounts),
			world:       clients.NewWorld(worldv1.NewWorldServiceClient(conns.world), log, rec),
			social:      clients.NewSocial(socialv1.NewSocialServiceClient(conns.social), log, rec),
			items:       clients.NewItems(itemv1.NewItemServiceClient(conns.item)),
			sessionEnds: clients.NewSessionEnds(accounts, log, rec),
		}, nil
	})
}

// Modules are the modules the gateway calls in the same process.
type Modules struct {
	Account   local.AccountModule
	Character local.CharacterModule
	Item      local.ItemModule
	World     local.WorldModule
	Social    local.SocialModule
}

// BuildLocal adds the gateway module to h from cfg, serving players at public and calling the modules in the same process.
func BuildLocal(ctx context.Context, h *host.Host, cfg config.Config, public host.HTTPSection, mods Modules) error {
	return build(ctx, h, cfg, public, func(log *slog.Logger, rec *panics.Recorder) (ports, error) {
		return ports{
			accounts:    local.NewAccounts(mods.Account),
			characters:  local.NewCharacters(mods.Character),
			guard:       local.NewGuard(mods.Account),
			world:       local.NewWorld(mods.World, log, rec),
			social:      local.NewSocial(mods.Social, log, rec),
			items:       local.NewItems(mods.Item),
			sessionEnds: local.NewSessionEnds(mods.Account, log, rec),
		}, nil
	})
}

// ports are the modules as the gateway's use cases call them, over gRPC or in the process.
type ports struct {
	accounts   application.Accounts
	characters application.Characters
	guard      interface {
		application.Sessions
		send.Sessions
		push.Sessions
	}
	world interface {
		session.World
		send.World
		push.World
	}
	social interface {
		session.Social
		send.Social
		push.Social
	}
	items interface {
		session.Items
		send.Items
	}
	sessionEnds push.Account
}

// build adds the gateway module over the ports connect makes, with the module's logger and panic recorder.
func build(ctx context.Context, h *host.Host, cfg config.Config, public host.HTTPSection, connect func(log *slog.Logger, rec *panics.Recorder) (ports, error)) error {
	m, err := h.Module("gateway")
	if err != nil {
		return err
	}
	m.TraceRoots(entryPoint)
	log, rec := m.Logger, m.Panics
	meters, err := metrics.New(h.Registry())
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	p, err := connect(log, rec)
	if err != nil {
		return err
	}
	lobby := application.NewLobby(application.LobbyDeps{Accounts: p.accounts, Characters: p.characters, Sessions: p.guard})
	sessions := session.NewRegistry(session.Deps{World: p.world, Social: p.social, Items: p.items, Logger: log, Panics: rec, Traces: h.Traces, Observer: meters}, session.Options{Gateway: uuidv7.NewString(), JoinTimeout: cfg.Sessions.JoinTimeout})
	sendFlow := send.New(send.Deps{
		Sessions: p.guard,
		Registry: sessions,
		World:    p.world,
		Items:    p.items,
		Social:   p.social,
		Logger:   log,
		Observer: meters,
	}, send.Options{JoinTimeout: cfg.Sessions.JoinTimeout})
	pushFlow := push.New(push.Deps{
		Sessions: p.guard,
		Registry: sessions,
		World:    p.world,
		Social:   p.social,
		Account:  p.sessionEnds,
		Logger:   log,
		Panics:   rec,
		Traces:   h.Traces,
	}, push.Options{SessionCheckInterval: cfg.Sessions.CheckInterval})
	h.Serve(httpListener, public.Addr, handler.NewRouter(handler.Routes{
		API: api.Mount(api.Dependencies{Lobby: lobby, Logger: log, CORSOrigins: public.CORSOrigins, Panics: rec, Traces: h.Traces}),
		Gateway: ws.New(sendFlow, ws.Options{
			Origins: public.CORSOrigins, HelloTimeout: cfg.WS.HelloTimeout, CommandQueue: cfg.WS.CommandQueue,
			Observer: meters, Logger: log, Panics: rec, Traces: h.Traces,
		}),
	}))
	m.Task("streams", pushFlow.Run, host.Restartable)
	h.OnDrain(pushFlow.Drain)
	log.InfoContext(ctx, "built")
	return nil
}

// entryPoint keeps the traces of REST requests and of the commands that answer after calls across services.
func entryPoint(span string) bool {
	return api.EntryPoint(span) || receive.EntryPoint(span)
}

type conns struct {
	account, character, item, world, social *grpc.ClientConn
}

// dialAll makes a connection to each service, which h closes; nothing connects until the first call.
func dialAll(h *host.Host, s config.Dependencies) (conns, error) {
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
		conn, err := h.Dial(d.name, d.addr)
		if err != nil {
			return conns{}, err
		}
		*d.conn = conn
	}
	return c, nil
}
