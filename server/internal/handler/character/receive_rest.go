package character

import (
	"context"
	"net/http"

	"finga/internal/application/character"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"

	"github.com/gin-gonic/gin"
)

// rest serves a player's characters over REST: client to server.
type rest struct {
	characters Characters
}

// Characters is the character module as its REST routes call it.
type Characters interface {
	List(ctx context.Context, account kernel.AccountID) ([]character.Character, error)
	Create(ctx context.Context, account kernel.AccountID, name string) (character.Character, error)
	SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(characters Characters) func(transport.REST) {
	h := &rest{characters: characters}
	return func(r transport.REST) {
		r.Authed.GET("/characters", h.listCharacters)
		r.Authed.POST("/characters", h.createCharacter)
		r.Authed.PUT("/characters/:id/skill-bar", h.setSkillBar)
	}
}

func (h *rest) listCharacters(c *gin.Context) {
	cs, err := h.characters.List(c.Request.Context(), transport.PrincipalOf(c).AccountID)
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
	ch, err := h.characters.Create(c.Request.Context(), transport.PrincipalOf(c).AccountID, req.Name)
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
	if err := h.characters.SetSkillBar(c.Request.Context(), id, transport.PrincipalOf(c).AccountID, req.Slots); err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, SkillBarResponse(req))
}
