package transport_test

import (
	"github.com/gin-gonic/gin"

	haccount "finga/internal/handler/account"
	hcharacter "finga/internal/handler/character"
	hitem "finga/internal/handler/item"
	hpvp "finga/internal/handler/pvp"
	"finga/internal/handler/transport"
)

// mountREST serves the whole REST API, each module's routes on the transport, as the wiring does.
func mountREST(d transport.RESTOptions) func(*gin.Engine) {
	return transport.MountREST(d, haccount.RESTRoutes(d.Lobby), hcharacter.RESTRoutes(d.Lobby), hitem.RESTRoutes(d.Lobby), hpvp.RESTRoutes(d.Lobby))
}
