package pvp

import (
	"context"
	"net/http"

	"finga/internal/application/pvp"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"

	"github.com/gin-gonic/gin"
)

// rest serves a character's arena profile, build and the boards over REST: client to server.
type rest struct {
	arena Arena
}

// Arena is the pvp module as its REST routes call it.
type Arena interface {
	Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (pvp.Profile, error)
	SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string, traits map[string]string) error
	Leaderboard(ctx context.Context, mode string) ([]pvp.Row, error)
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(arena Arena) func(transport.REST) {
	h := &rest{arena: arena}
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
	p, err := h.arena.Profile(c.Request.Context(), transport.PrincipalOf(c).AccountID, id)
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
	b := pvp.Build{Bar: req.SkillBar, Traits: req.Traits}
	if err := h.arena.SetBuild(c.Request.Context(), transport.PrincipalOf(c).AccountID, id, b.Bar, b.Traits); err != nil {
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
	rows, err := h.arena.Leaderboard(c.Request.Context(), mode)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toLeaderboard(mode, rows))
}
