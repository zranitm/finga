package handler

import (
	"sync"

	"github.com/gin-gonic/gin"
)

var releaseMode sync.Once

// NewEngine is a Gin engine as the gateway runs it (TD-26): release mode
// (Gin's debug mode prints every route at start, through its own logger),
// no redirects (a path that is not a route is a 404, never a redirect to
// one), and 405 for a known path asked with another method. Middleware and
// the not-found answers are the REST API's (handler/api).
func NewEngine() *gin.Engine {
	releaseMode.Do(func() { gin.SetMode(gin.ReleaseMode) }) // Gin keeps its mode in a package variable
	e := gin.New()
	e.HandleMethodNotAllowed = true
	e.RedirectTrailingSlash = false
	e.RedirectFixedPath = false
	// No proxy is trusted, so Gin's ClientIP never reads a forwarding header
	// (the gateway takes the socket's address, decision 253).
	if err := e.SetTrustedProxies(nil); err != nil {
		panic("gin: " + err.Error()) // nil is always a valid list
	}
	return e
}
