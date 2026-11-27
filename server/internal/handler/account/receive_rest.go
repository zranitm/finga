package account

import (
	"net/http"

	"github.com/gin-gonic/gin"

	application "finga/internal/application/gateway"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
)

// rest serves accounts, login sessions and settings over REST: client to server.
type rest struct {
	lobby *application.Lobby
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(lobby *application.Lobby) func(transport.REST) {
	h := &rest{lobby: lobby}
	return func(r transport.REST) {
		r.Public.POST("/accounts", h.register)
		r.Public.POST("/sessions", h.login)
		r.Authed.DELETE("/sessions/current", h.logout)
		r.Authed.DELETE("/sessions", h.logoutEverywhere)
		r.Authed.GET("/settings", h.settings)
		r.Authed.PUT("/settings", h.updateSettings)
	}
}

func (h *rest) register(c *gin.Context) {
	var req RegisterRequest
	if transport.Decode(c, &req) != nil || transport.Missing(c, req.Username, req.Password) {
		return
	}
	acc, err := h.lobby.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusCreated, toAccount(acc))
}

func (h *rest) login(c *gin.Context) {
	var req LoginRequest
	if transport.Decode(c, &req) != nil || transport.Missing(c, req.Username, req.Password) {
		return
	}
	token, err := h.lobby.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusCreated, SessionResponse{Token: token})
}

func (h *rest) logout(c *gin.Context) {
	if err := h.lobby.Logout(c.Request.Context(), transport.PrincipalOf(c)); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *rest) logoutEverywhere(c *gin.Context) {
	if err := h.lobby.LogoutEverywhere(c.Request.Context(), transport.PrincipalOf(c)); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *rest) settings(c *gin.Context) {
	s, err := h.lobby.Settings(c.Request.Context(), transport.PrincipalOf(c))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toSettings(s))
}

func (h *rest) updateSettings(c *gin.Context) {
	var req SettingsRequest
	if transport.Decode(c, &req) != nil {
		return
	}
	s := model.Settings{AutoAssignPoints: req.AutoAssignPoints, ChatPinned: req.ChatPinned, HideMinimap: req.HideMinimap, Zoom: req.Zoom, Walk: req.Walk, Keys: req.Keys}
	if err := h.lobby.UpdateSettings(c.Request.Context(), transport.PrincipalOf(c), s); err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toSettings(s))
}
