package account

import (
	"context"
	"net/http"

	"finga/internal/application/account"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"

	"github.com/gin-gonic/gin"
)

// Accounts is the account module as its REST routes call it.
type Accounts interface {
	Register(ctx context.Context, c account.Credentials) (account.Account, error)
	Login(ctx context.Context, c account.Credentials) (string, error)
	Revoke(ctx context.Context, id kernel.SessionID) error
	RevokeAll(ctx context.Context, id kernel.AccountID) error
	Settings(ctx context.Context, id kernel.AccountID) (account.Settings, error)
	UpdateSettings(ctx context.Context, id kernel.AccountID, s account.Settings) error
}

// rest serves accounts, login sessions and settings over REST: client to server.
type rest struct {
	accounts Accounts
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(accounts Accounts) func(transport.REST) {
	h := &rest{accounts: accounts}
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
	acc, err := h.accounts.Register(c.Request.Context(), account.Credentials{Username: req.Username, Password: req.Password})
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
	token, err := h.accounts.Login(c.Request.Context(), account.Credentials{Username: req.Username, Password: req.Password})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusCreated, SessionResponse{Token: token})
}

func (h *rest) logout(c *gin.Context) {
	if err := h.accounts.Revoke(c.Request.Context(), transport.PrincipalOf(c).SessionID); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *rest) logoutEverywhere(c *gin.Context) {
	if err := h.accounts.RevokeAll(c.Request.Context(), transport.PrincipalOf(c).AccountID); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *rest) settings(c *gin.Context) {
	s, err := h.accounts.Settings(c.Request.Context(), transport.PrincipalOf(c).AccountID)
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
	s := settingsOf(req)
	if err := h.accounts.UpdateSettings(c.Request.Context(), transport.PrincipalOf(c).AccountID, s); err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toSettings(s))
}
