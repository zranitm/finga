package item

import (
	"context"
	"net/http"

	"finga/internal/application/item"
	"finga/internal/handler/transport"
	"finga/internal/shared/kernel"

	"github.com/gin-gonic/gin"
)

// rest serves an account's wardrobe over REST: client to server.
type rest struct {
	items Wardrobes
}

// Wardrobes is the item module as its REST routes call it.
type Wardrobes interface {
	Wardrobe(ctx context.Context, account kernel.AccountID) ([]item.Piece, error)
}

// RESTRoutes adds the module's REST routes to the API.
func RESTRoutes(items Wardrobes) func(transport.REST) {
	h := &rest{items: items}
	return func(r transport.REST) {
		r.Authed.GET("/wardrobe", h.wardrobe)
	}
}

func (h *rest) wardrobe(c *gin.Context) {
	pieces, err := h.items.Wardrobe(c.Request.Context(), transport.PrincipalOf(c).AccountID)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	transport.WriteJSON(c, http.StatusOK, toWardrobe(pieces))
}
