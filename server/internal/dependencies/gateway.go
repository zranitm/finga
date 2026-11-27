package dependencies

import (
	"context"
	"errors"
	application "finga/internal/application/account"
	"fmt"

	"finga/config"
	adapter "finga/internal/adapter/account"
	handleraccount "finga/internal/handler/account"
	handlercharacter "finga/internal/handler/character"
	handleritem "finga/internal/handler/item"
	handlerpvp "finga/internal/handler/pvp"
	handlersocial "finga/internal/handler/social"
	"finga/internal/handler/transport"
	handlerworld "finga/internal/handler/world"
	metricsaccount "finga/internal/metrics/account"
	metricstransport "finga/internal/metrics/transport"
	"finga/internal/shared/host"
	"finga/pkg/ratelimit"
	"finga/pkg/uuidv7"
)

// The players' listener's name, as the logs show it.
const httpListener = "http"

// gatewayModules are the modules the gateway calls, in the same process.
type gatewayModules struct {
	Account   adapter.OwnModule
	Character handlercharacter.Characters
	Item      adapter.ItemModule
	Wardrobes handleritem.Wardrobes
	World     adapter.WorldModule
	Social    adapter.SocialModule
	Pvp       adapter.PvpModule
	Arena     handlerpvp.Arena
}

// buildGateway adds the gateway module to h from cfg, serving players at public and calling mods.
func buildGateway(ctx context.Context, h *host.Host, cfg config.Gateway, public host.HTTPSection, mods gatewayModules) error {
	m, err := h.Module("gateway")
	if err != nil {
		return err
	}
	m.TraceRoots(entryPoint)
	log, rec := m.Logger, m.Panics
	online, err := metricsaccount.New(h.Registry())
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	socket, err := metricstransport.New(h.Registry())
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	guard, world, social, items := adapter.NewGuard(mods.Account), adapter.NewWorld(mods.World, log, rec), adapter.NewSocial(mods.Social, log, rec), adapter.NewItems(mods.Item)
	matches := adapter.NewPvp(mods.Pvp, log, rec)
	sessions := application.NewConnections(application.ConnectionsDeps{World: world, Social: social, Pvp: matches, Items: items, Logger: log, Panics: rec, Traces: h.Traces, Observer: online}, application.ConnectionsOptions{Gateway: uuidv7.NewString(), JoinTimeout: cfg.Sessions.JoinTimeout})
	sendFlow := application.NewCommands(application.CommandsDeps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Items:    items,
		Social:   social,
		Pvp:      matches,
		Logger:   log,
		Observer: online,
	}, application.CommandsOptions{JoinTimeout: cfg.Sessions.JoinTimeout})
	pushFlow := application.NewStreams(application.StreamsDeps{
		Sessions: guard,
		Registry: sessions,
		World:    world,
		Social:   social,
		Pvp:      matches,
		Account:  adapter.NewSessionEnds(mods.Account, log, rec),
		Logger:   log,
		Panics:   rec,
		Traces:   h.Traces,
	}, application.StreamsOptions{SessionCheckInterval: cfg.Sessions.CheckInterval})
	encoders := transport.NewEncoders()
	if err := errors.Join(handleraccount.Encoders(encoders), handlercharacter.Encoders(encoders), handleritem.Encoders(encoders), handlerpvp.Encoders(encoders), handlersocial.Encoders(encoders), handlerworld.Encoders(encoders)); err != nil {
		return fmt.Errorf("push encoders: %w", err)
	}
	ws, err := transport.NewServer(sendFlow, transport.ServerOptions{
		Origins: public.CORSOrigins, HelloTimeout: cfg.WS.HelloTimeout, CommandQueue: cfg.WS.CommandQueue,
		Observer: socket, Logger: log, Panics: rec, Traces: h.Traces,
	}, encoders, handleraccount.WSRoutes(sendFlow), handleritem.WSRoutes(sendFlow), handlerpvp.WSRoutes(sendFlow), handlersocial.WSRoutes(sendFlow), handlerworld.WSRoutes(sendFlow))
	if err != nil {
		return fmt.Errorf("websocket routes: %w", err)
	}
	h.Serve(httpListener, public.Addr, transport.NewRouter(transport.HTTPRoutes{
		API: transport.MountREST(transport.RESTOptions{
			Sessions: mods.Account, Logger: log, CORSOrigins: public.CORSOrigins, Panics: rec, Traces: h.Traces,
			Limit: ratelimit.NewBuckets(cfg.REST.PerSecond, cfg.REST.Burst),
		}, handleraccount.RESTRoutes(mods.Account), handlercharacter.RESTRoutes(mods.Character), handleritem.RESTRoutes(mods.Wardrobes), handlerpvp.RESTRoutes(mods.Arena)),
		WS: ws,
	}))
	m.Task("streams", pushFlow.Run, host.Restartable)
	h.OnDrain(pushFlow.Drain)
	log.InfoContext(ctx, "built")
	return nil
}

// entryPoint keeps the traces of REST requests and of the commands that answer after calls across modules.
func entryPoint(span string) bool {
	return transport.HTTPEntryPoint(span) || transport.WSEntryPoint(span)
}
