package handler

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

func registerTjuptFeed(g *gin.RouterGroup, feed *migrationflow.TjuptFeed, transition *sync.Mutex,
	invoke func(*gin.Context, string)) {
	view := func(c *gin.Context) {
		tjuptFeedView(c, feed)
	}
	g.GET("", view)
	g.PUT("", func(c *gin.Context) {
		var input migrationflow.TjuptFeedInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "北洋园插件配置格式无效"})
			return
		}
		if err := feed.Save(c.Request.Context(), input); err != nil {
			tjuptFeedError(c, err, http.StatusServiceUnavailable, "北洋园插件配置暂无法保存")
			return
		}
		view(c)
	})
	g.POST("/upload-limit", func(c *gin.Context) {
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if c.ShouldBindJSON(&input) != nil || input.Enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请选择开启或关闭站点上传限速"})
			return
		}
		if err := feed.SetSiteUploadLimit(c.Request.Context(), *input.Enabled); err != nil {
			tjuptFeedError(c, err, http.StatusServiceUnavailable, "站点上传限速开关暂无法保存")
			return
		}
		view(c)
	})
	for _, action := range []string{"preview", "status", "trial", "start"} {
		g.POST("/"+action, func(c *gin.Context) { invoke(c, action) })
	}
	// 紧急停止可直接调用宿主，外装包失效时仍可暂停循环。
	g.POST("/stop", func(c *gin.Context) {
		if !lockTjuptTransition(c, transition) {
			return
		}
		defer transition.Unlock()
		runTjuptFeedAction(c, feed, "stop")
	})
}

// tjuptFeedView 返回现有台账的脱敏设置与运行状态。
func tjuptFeedView(c *gin.Context, feed *migrationflow.TjuptFeed) {
	settings, err := feed.View(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "北洋园插件配置暂不可读取"})
		return
	}
	c.JSON(http.StatusOK, settings)
}

// runTjuptFeedAction 将固定动作分派到现有的北洋园服务。
func runTjuptFeedAction(c *gin.Context, feed *migrationflow.TjuptFeed, action string) {
	ctx := c.Request.Context()
	switch action {
	case "preview":
		items, err := feed.Preview(ctx)
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "暂无法读取北洋园 RSS，请检查已保存的链接")
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	case "status":
		items, err := feed.Status(ctx)
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "暂无法查询 H&R，请检查北洋园 Token 与下载器连接")
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	case "trial":
		result, err := feed.Trial(ctx)
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "北洋园试运行未能完成，请检查 RSS 与下载器连接")
			return
		}
		c.JSON(http.StatusOK, result)
	case "start", "stop":
		if err := feed.SetEnabled(ctx, action == "start"); err != nil {
			message := "北洋园循环暂无法停止"
			if action == "start" {
				message = "北洋园循环暂无法启动"
			}
			tjuptFeedError(c, err, http.StatusServiceUnavailable, message)
			return
		}
		tjuptFeedView(c, feed)
	default:
		c.JSON(http.StatusNotFound, gin.H{"message": "未找到北洋园操作"})
	}
}

func lockTjuptTransition(c *gin.Context, transition *sync.Mutex) bool {
	if transition.TryLock() {
		return true
	}
	c.JSON(http.StatusConflict, gin.H{"message": "北洋园插件有操作正在执行，请稍后再试"})
	return false
}

func tjuptFeedError(c *gin.Context, err error, status int, message string) {
	switch {
	case errors.Is(err, migrationflow.ErrTjuptFeedConfig):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, migrationflow.ErrTjuptFeedBusy):
		status = http.StatusConflict
		message = err.Error()
	}
	c.JSON(status, gin.H{"message": message})
}
