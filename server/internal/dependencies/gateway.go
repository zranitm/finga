package dependencies

import (
	"context"
	"fmt"

	"finga/config"
	adapter "finga/internal/adapter/gateway"
	application "finga/internal/application/gateway"
	"finga/internal/application/gateway/push"
	"finga/internal/application/gateway/send"
	"finga/internal/application/gateway/session"
	handler "finga/internal/handler/gateway"
	"finga/internal/handler/gateway/api"
	"finga/internal/handler/gateway/ws"
	"finga/internal/handler/gateway/ws/receive"
	metrics "finga/internal/metrics/gateway"
	"finga/internal/shared/host"
	"finga/pkg/ratelimit"
	"finga/pkg/uuidv7"
)

// The players' listener's name, as the logs show it.
const httpListener = "http"

// gatewayModules are the modules the gateway calls, in the same process.
type gatewayModules struct {
	Account   adapter.AccountModule
	Character adapter.CharacterModule
	Item      adapter.ItemModule
	World     adapter.WorldModule
	Social    adapter.SocialModule
	Pvp       adapter.PvpModule
	Arena     adapter.ArenaModule
}

// buildGateway adds the gateway module to h from cfg, serving players at public and calling mods.
func buildGateway(ctx context.Context, h *host.Host, cfg config.Gateway, public host.HTTPSection, mods gatewayModules) error {
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
	guard, world, social, items := adapter.NewGuard(mods.Account), adapter.NewWorld(mods.World, log, rec), adapter.NewSocial(mods.Social, log, rec), adapter.NewItems(mods.Item)
	matches := adapter.NewPvp(mods.Pvp, log, rec)
	lobby := application.NewLobby(application.LobbyDeps{Accounts: adapter.NewAccounts(mods.Account), Characters: adapter.NewCharacters(mods.Character), Sessions: guard, Wardrobes: items, Arena: adapter.NewArena(mods.Arena)})
	sessions := session.NewRegistry(session.Deps{World: world, Social: social, Pvp: matches, Items: items, Logger: log, Panics: rec, Traces: h.Traces, Observer: meters}, session.Options{Gateway: uuidv7.NewString(), JoinTimeout: cfg.Sessions.JoinTimeout})
	sendFlow := send.New(send.Deps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Items:    items,
		Social:   social,
		Pvp:      matches,
		Logger:   log,
		Observer: meters,
	}, send.Options{JoinTimeout: cfg.Sessions.JoinTimeout})
	pushFlow := push.New(push.Deps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Social:   social,
		Pvp:      matches,
		Account:  adapter.NewSessionEnds(mods.Account, log, rec),
		Logger:   log,
		Panics:   rec,
		Traces:   h.Traces,
	}, push.Options{SessionCheckInterval: cfg.Sessions.CheckInterval})
	h.Serve(httpListener, public.Addr, handler.NewRouter(handler.Routes{
		API: api.Mount(api.Dependencies{
			Lobby: lobby, Logger: log, CORSOrigins: public.CORSOrigins, Panics: rec, Traces: h.Traces,
			Limit: ratelimit.NewBuckets(cfg.REST.PerSecond, cfg.REST.Burst),
		}),
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
