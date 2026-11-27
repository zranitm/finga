package transport_test

import (
	"finga/internal/application/gateway/send"
	haccount "finga/internal/handler/account"
	hcharacter "finga/internal/handler/character"
	hitem "finga/internal/handler/item"
	hpvp "finga/internal/handler/pvp"
	hsocial "finga/internal/handler/social"
	"finga/internal/handler/transport"
	hworld "finga/internal/handler/world"
)

// allEncoders are the push encoders of every module's handler, as the wiring registers them.
func allEncoders() *transport.Encoders {
	enc := transport.NewEncoders()
	for _, register := range []func(*transport.Encoders) error{haccount.Encoders, hcharacter.Encoders, hitem.Encoders, hpvp.Encoders, hsocial.Encoders, hworld.Encoders} {
		if err := register(enc); err != nil {
			panic(err) // a test's wiring, which no case of a test can recover
		}
	}
	return enc
}

// newServer is the WebSocket server over every module's routes and encoders, as the wiring builds it.
func newServer(gw *send.Gateway, opts transport.ServerOptions) *transport.Server {
	srv, err := transport.NewServer(gw, opts, allEncoders(), haccount.WSRoutes(gw), hitem.WSRoutes(gw), hpvp.WSRoutes(gw), hsocial.WSRoutes(gw), hworld.WSRoutes(gw))
	if err != nil {
		panic(err)
	}
	return srv
}
