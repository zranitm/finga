package item

import (
	"net/http"

	"github.com/gin-gonic/gin"

	application "finga/internal/application/gateway"
	"finga/internal/handler/transport"
)

// rest serves an account's wardrobe over REST: client to server.
type rest struct {
	lobby *application.Lobby
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(lobby *application.Lobby) func(transport.REST) {
	h := &rest{lobby: lobby}
	return func(r transport.REST) {
		r.Authed.GET("/wardrobe", h.wardrobe)
	}
}

func (h *rest) wardrobe(c *gin.Context) {
	pieces, err := h.lobby.Wardrobe(c.Request.Context(), transport.PrincipalOf(c))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toWardrobe(pieces))
}
