package handler

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/repository"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

// RegisterTjuptRSS 挂载插件的管理员接口。
func RegisterTjuptRSS(g *gin.RouterGroup, service *migrationflow.TjuptRSSService, automation *migrationflow.TjuptAutomation,
	feed *migrationflow.TjuptFeed, transition *sync.Mutex, invokeFeed func(*gin.Context, string)) {
	g.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		c.Next()
	})
	registerTjuptFeed(g.Group("/feed"), feed, transition, invokeFeed)
	g.GET("/downloaders", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"items": service.Downloaders(), "retire_configured": service.RetireConfigured()})
	})
	g.POST("/preview", func(c *gin.Context) {
		var req struct {
			RSSURL string   `json:"rss_url"`
			MinGiB float64  `json:"min_gib"`
			MaxGiB *float64 `json:"max_gib"`
		}
		if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.RSSURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写北洋园 RSS 地址及有效的体积范围"})
			return
		}
		limit := 10.0
		if req.MaxGiB != nil {
			limit = *req.MaxGiB
		}
		items, err := service.PreviewRange(c.Request.Context(), req.RSSURL, req.MinGiB, limit)
		if err != nil {
			if errors.Is(err, migrationflow.ErrTjuptPolicy) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "请检查单种最小和最大体积"})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
	g.POST("/status", func(c *gin.Context) {
		var req tjuptStatusRequest
		if !bindTjuptStatus(c, &req) {
			return
		}
		items, err := service.Status(c.Request.Context(), req.DownloaderID, req.Token)
		if err != nil {
			tjuptRequestError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
	g.POST("/retire", func(c *gin.Context) {
		var req tjuptStatusRequest
		if !bindTjuptStatus(c, &req) {
			return
		}
		if err := service.Retire(c.Request.Context(), req.DownloaderID, req.Token, req.Hash); err != nil {
			tjuptRequestError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	viewAutomation := func(c *gin.Context) {
		view, err := automation.View(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "北洋园自动化台账暂不可读取"})
			return
		}
		c.JSON(http.StatusOK, view)
	}
	g.GET("/automation", viewAutomation)
	g.PUT("/automation", func(c *gin.Context) {
		var settings migrationflow.TjuptAutomationSettings
		if c.ShouldBindJSON(&settings) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "北洋园规则格式无效"})
			return
		}
		if !lockTjuptTransition(c, transition) {
			return
		}
		defer transition.Unlock()
		if settings.Enabled {
			current, err := feed.View(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"message": "北洋园插件状态暂不可读取"})
				return
			}
			if current.Enabled || current.Running {
				c.JSON(http.StatusConflict, gin.H{"message": "请先停止插件的 RSS 循环"})
				return
			}
		}
		if err := automation.Save(c.Request.Context(), settings); err != nil {
			switch {
			case errors.Is(err, migrationflow.ErrTjuptPolicy):
				c.JSON(http.StatusBadRequest, gin.H{"message": strings.ReplaceAll(err.Error(), "\n", "。")})
			case errors.Is(err, repository.ErrTjuptAutomationBusy):
				c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
			default:
				c.JSON(http.StatusServiceUnavailable, gin.H{"message": "北洋园规则暂无法保存"})
			}
			return
		}
		viewAutomation(c)
	})
}

type tjuptStatusRequest struct {
	DownloaderID string `json:"downloader_id"`
	Token        string `json:"token"`
	Hash         string `json:"hash"`
}

// bindTjuptStatus 验证查询条件，失败时只返回固定提示。
func bindTjuptStatus(c *gin.Context, req *tjuptStatusRequest) bool {
	if c.ShouldBindJSON(req) != nil || strings.TrimSpace(req.DownloaderID) == "" ||
		strings.TrimSpace(req.Token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择飞牛 qB 下载器并输入北洋园 API Token"})
		return false
	}
	return true
}

// tjuptRequestError 区分退种未获许可与上游连接失败。
func tjuptRequestError(c *gin.Context, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, migrationflow.ErrTjuptDownloader) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, migrationflow.ErrTjuptRetireDenied) || errors.Is(err, migrationflow.ErrTjuptRetireDisabled) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"message": err.Error()})
}
