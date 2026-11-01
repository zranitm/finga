package handler

import (
	"sync"

	"github.com/gin-gonic/gin"
)

var releaseMode sync.Once

// NewEngine is a Gin engine in release mode, with no redirects and 405 for a known path with another method.
func NewEngine() *gin.Engine {
	releaseMode.Do(func() { gin.SetMode(gin.ReleaseMode) }) // Gin keeps its mode in a package variable
	e := gin.New()
	e.HandleMethodNotAllowed = true
	e.RedirectTrailingSlash = false
	e.RedirectFixedPath = false
	// Trust no proxy, so ClientIP never reads a forwarding header; per-IP limits are the edge's.
	if err := e.SetTrustedProxies(nil); err != nil {
		panic("gin: " + err.Error()) // nil is always a valid list
	}
	return e
}
