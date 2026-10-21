package operations

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/twistingmercury/heartbeat/v2"
	"github.com/twistingmercury/librarian/internal/enricher/version"
)

// SetupHandlers associates the handlers related to operations endpoints
// to the gin.Engine that is passed in. The deps slice is used to register
// real dependency health checks with the heartbeat handler.
func SetupHandlers(r *gin.Engine, deps []heartbeat.DependencyDescriptor) {
	r.GET("/health", gin.WrapH(heartbeat.Handler(version.Service, deps...)))
	r.GET("/version", GetVersion)
}

// GetVersion handles GET /version
// See api/openapi/mnemonic-v1.yaml:2276
//
// @Summary      Get service version
// @Tags         Operations
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /version [get]
func GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":    version.Service,
		"version":    version.Version(),
		"build_date": version.BuildDate(),
		"commit":     version.Commit(),
	})
}
