package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/plugins"
	"gorm.io/gorm"
)

// RegisterPlugins 提供内置插件和管理员安装的本地清单。
func RegisterPlugins(g *gin.RouterGroup, dataDir string, db *gorm.DB) {
	catalog := plugins.New(dataDir, plugins.Manifest{
		SchemaVersion: 1,
		ID:            "tjupt-rss",
		Name:          "北洋园刷流",
		Version:       "1.0",
		Description:   "订阅北洋园 RSS，在 qB 试运行或循环下载，并查看 H&R 剩余时间。",
		Blocks:        []plugins.Block{},
	})
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
