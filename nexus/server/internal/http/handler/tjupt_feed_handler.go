package handler

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

func registerTjuptFeed(g *gin.RouterGroup, feed *migrationflow.TjuptFeed, transition *sync.Mutex) {
	view := func(c *gin.Context) {
		settings, err := feed.View(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "北洋园插件配置暂不可读取"})
			return
		}
		c.JSON(http.StatusOK, settings)
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
	g.POST("/trial", func(c *gin.Context) {
		if !lockTjuptTransition(c, transition) {
			return
		}
		defer transition.Unlock()
		result, err := feed.Trial(c.Request.Context())
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "北洋园试运行未能完成，请检查 RSS 与下载器连接")
			return
		}
		c.JSON(http.StatusOK, result)
	})
	g.POST("/start", func(c *gin.Context) {
		if !lockTjuptTransition(c, transition) {
			return
		}
		defer transition.Unlock()
		if err := feed.SetEnabled(c.Request.Context(), true); err != nil {
			tjuptFeedError(c, err, http.StatusServiceUnavailable, "北洋园循环暂无法启动")
			return
		}
		view(c)
	})
	g.POST("/stop", func(c *gin.Context) {
		if !lockTjuptTransition(c, transition) {
			return
		}
		defer transition.Unlock()
		if err := feed.SetEnabled(c.Request.Context(), false); err != nil {
			tjuptFeedError(c, err, http.StatusServiceUnavailable, "北洋园循环暂无法停止")
			return
		}
		view(c)
	})
	g.POST("/preview", func(c *gin.Context) {
		items, err := feed.Preview(c.Request.Context())
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "暂无法读取北洋园 RSS，请检查已保存的链接")
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
	g.POST("/status", func(c *gin.Context) {
		items, err := feed.Status(c.Request.Context())
		if err != nil {
			tjuptFeedError(c, err, http.StatusBadGateway, "暂无法查询 H&R，请检查北洋园 Token 与下载器连接")
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
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
