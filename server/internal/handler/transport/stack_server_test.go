package transport_test

import (
	application "finga/internal/application/account"
	haccount "finga/internal/handler/account"
	hitem "finga/internal/handler/item"
	hpvp "finga/internal/handler/pvp"
	hsocial "finga/internal/handler/social"
	"finga/internal/handler/transport"
	hworld "finga/internal/handler/world"
)

// allEncoders are the push encoders of every module's handler, as the wiring registers them.
func allEncoders() *transport.Encoders {
	enc := transport.NewEncoders()
	for _, register := range []func(*transport.Encoders) error{haccount.Encoders, hitem.Encoders, hpvp.Encoders, hsocial.Encoders, hworld.Encoders} {
		if err := register(enc); err != nil {
			panic(err) // a test's wiring, which no case of a test can recover
		}
	}
	return enc
}

// newServer is the WebSocket server over every module's routes and encoders, as the wiring builds it.
func newServer(gw *application.Commands, m deps, opts transport.ServerOptions) *transport.Server {
	srv, err := transport.NewServer(gw, opts, allEncoders(), haccount.WSRoutes(gw), hitem.WSRoutes(m.Items), hpvp.WSRoutes(m.Pvp), hsocial.WSRoutes(m.Social, gw), hworld.WSRoutes(m.World))
	if err != nil {
		panic(err)
	}
	return srv
}
