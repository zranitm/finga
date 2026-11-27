package transport_test

import (
	"github.com/gin-gonic/gin"

	haccount "finga/internal/handler/account"
	hcharacter "finga/internal/handler/character"
	hitem "finga/internal/handler/item"
	hpvp "finga/internal/handler/pvp"
	"finga/internal/handler/transport"
)

// restModules are the modules the REST routes call.
type restModules struct {
	Accounts   haccount.Accounts
	Characters hcharacter.Characters
	Wardrobes  hitem.Wardrobes
	Arena      hpvp.Arena
}

// mountREST serves the whole REST API, each module's routes on the transport, as the wiring does.
func mountREST(d transport.RESTOptions, m restModules) func(*gin.Engine) {
	return transport.MountREST(d, haccount.RESTRoutes(m.Accounts), hcharacter.RESTRoutes(m.Characters), hitem.RESTRoutes(m.Wardrobes), hpvp.RESTRoutes(m.Arena))
}
