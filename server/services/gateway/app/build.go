// Package app is the gateway's composition root, over the modules in the same process.
package app

import (
	"context"
	"fmt"

	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/gateway/config"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/application/push"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/handler"
	"finga/services/gateway/internal/handler/api"
	"finga/services/gateway/internal/handler/ws"
	"finga/services/gateway/internal/handler/ws/receive"
	"finga/services/gateway/internal/local"
	"finga/services/gateway/internal/metrics"
)

// The players' listener's name, as the logs show it.
const httpListener = "http"

// Modules are the modules the gateway calls, in the same process.
type Modules struct {
	Account   local.AccountModule
	Character local.CharacterModule
	Item      local.ItemModule
	World     local.WorldModule
	Social    local.SocialModule
}

// Build adds the gateway module to h from cfg, serving players at public and calling mods.
func Build(ctx context.Context, h *host.Host, cfg config.Config, public host.HTTPSection, mods Modules) error {
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
	guard, world, social, items := local.NewGuard(mods.Account), local.NewWorld(mods.World, log, rec), local.NewSocial(mods.Social, log, rec), local.NewItems(mods.Item)
	lobby := application.NewLobby(application.LobbyDeps{Accounts: local.NewAccounts(mods.Account), Characters: local.NewCharacters(mods.Character), Sessions: guard})
	sessions := session.NewRegistry(session.Deps{World: world, Social: social, Items: items, Logger: log, Panics: rec, Traces: h.Traces, Observer: meters}, session.Options{Gateway: uuidv7.NewString(), JoinTimeout: cfg.Sessions.JoinTimeout})
	sendFlow := send.New(send.Deps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Items:    items,
		Social:   social,
		Logger:   log,
		Observer: meters,
	}, send.Options{JoinTimeout: cfg.Sessions.JoinTimeout})
	pushFlow := push.New(push.Deps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Social:   social,
		Account:  local.NewSessionEnds(mods.Account, log, rec),
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

// entryPoint keeps the traces of REST requests and of the commands that answer after calls across modules.
func entryPoint(span string) bool {
	return api.EntryPoint(span) || receive.EntryPoint(span)
}
