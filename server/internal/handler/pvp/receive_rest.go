package pvp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	application "finga/internal/application/gateway"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
)

// rest serves a character's arena profile, build and the boards over REST: client to server.
type rest struct {
	lobby *application.Lobby
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(lobby *application.Lobby) func(transport.REST) {
	h := &rest{lobby: lobby}
	return func(r transport.REST) {
		r.Authed.GET("/characters/:id/pvp", h.pvpProfile)
		r.Authed.PUT("/characters/:id/pvp-build", h.setArenaBuild)
		r.Authed.GET("/pvp/leaderboard", h.leaderboard)
	}
}

func (h *rest) pvpProfile(c *gin.Context) {
	id, err := kernel.ParseCharacterID(c.Param("id"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	p, err := h.lobby.PvpProfile(c.Request.Context(), transport.PrincipalOf(c), id)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toPvpProfile(p))
}

func (h *rest) setArenaBuild(c *gin.Context) {
	id, err := kernel.ParseCharacterID(c.Param("id"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	var req ArenaBuildRequest
	if transport.Decode(c, &req) != nil {
		return
	}
	b := model.ArenaBuild{Bar: req.SkillBar, Traits: req.Traits}
	if err := h.lobby.SetArenaBuild(c.Request.Context(), transport.PrincipalOf(c), id, b); err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toArenaBuild(b))
}

func (h *rest) leaderboard(c *gin.Context) {
	mode := c.Query("mode")
	if transport.Missing(c, mode) {
		return
	}
	rows, err := h.lobby.Leaderboard(c.Request.Context(), mode)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toLeaderboard(mode, rows))
}
