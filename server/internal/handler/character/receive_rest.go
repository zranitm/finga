package character

import (
	"net/http"

	"github.com/gin-gonic/gin"

	application "finga/internal/application/gateway"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"
)

// rest serves a player's characters over REST: client to server.
type rest struct {
	lobby *application.Lobby
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(lobby *application.Lobby) func(transport.REST) {
	h := &rest{lobby: lobby}
	return func(r transport.REST) {
		r.Authed.GET("/characters", h.listCharacters)
		r.Authed.POST("/characters", h.createCharacter)
		r.Authed.PUT("/characters/:id/skill-bar", h.setSkillBar)
	}
}

func (h *rest) listCharacters(c *gin.Context) {
	cs, err := h.lobby.Characters(c.Request.Context(), transport.PrincipalOf(c))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toCharacters(cs))
}

func (h *rest) createCharacter(c *gin.Context) {
	var req CreateCharacterRequest
	if transport.Decode(c, &req) != nil || transport.Missing(c, req.Name) {
		return
	}
	ch, err := h.lobby.CreateCharacter(c.Request.Context(), transport.PrincipalOf(c), req.Name)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusCreated, toCharacter(ch))
}

func (h *rest) setSkillBar(c *gin.Context) {
	id, err := kernel.ParseCharacterID(c.Param("id"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	var req SkillBarRequest
	if transport.Decode(c, &req) != nil {
		return
	}
	if err := h.lobby.SetSkillBar(c.Request.Context(), transport.PrincipalOf(c), id, req.Slots); err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, SkillBarResponse(req))
}
