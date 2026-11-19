package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/plugins"
	"gorm.io/gorm"
)

// RegisterPlugins mounts the local manifest directory under the administrator API.
func RegisterPlugins(g *gin.RouterGroup, dataDir string, db *gorm.DB) {
	catalog := plugins.New(dataDir)
	_, _ = catalog.Reload()
	respond := func(c *gin.Context, snapshot plugins.Snapshot) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"items": snapshot.Items, "issues": snapshot.Issues,
			"metrics": plugins.Counts(c.Request.Context(), db, snapshot),
		})
	}
	g.GET("", func(c *gin.Context) { respond(c, catalog.List()) })
	g.POST("/refresh", func(c *gin.Context) {
		snapshot, err := catalog.Reload()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": err.Error()})
			return
		}
		respond(c, snapshot)
	})
}
