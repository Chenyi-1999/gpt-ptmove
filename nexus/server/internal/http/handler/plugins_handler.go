package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
	"github.com/pt-nexus/server/internal/service/plugins"
	"github.com/pt-nexus/server/internal/service/torrentdata"
	"gorm.io/gorm"
)

const automationPluginID = "automation"

type automationPluginControl interface {
	PluginEnabled(context.Context) (bool, error)
	SetPluginEnabled(context.Context, bool) error
}

// PluginDownloaderSummary contains cached counters, with no downloader URLs or credentials.
type PluginDownloaderSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Uploaded   int64  `json:"uploaded_bytes"`
	Downloaded int64  `json:"downloaded_bytes"`
}

// TjuptPluginServices 保存北洋园外装包可以调用的宿主服务。
type TjuptPluginServices struct {
	RSS        *migrationflow.TjuptRSSService
	Automation *migrationflow.TjuptAutomation
	Feed       *migrationflow.TjuptFeed
}

type pluginHandler struct {
	catalog          *plugins.Catalog
	packages         *plugins.PackageManager
	db               *gorm.DB
	automation       automationPluginControl
	downloaders      func(context.Context) ([]PluginDownloaderSummary, error)
	inventory        *torrentdata.DownloaderInventory
	inventoryList    func(bool) []map[string]any
	refreshMu        sync.Mutex
	tjupt            *migrationflow.TjuptFeed
	tjuptTransition  sync.Mutex
	schedulesChanged chan struct{}
}

// RegisterPlugins 挂载插件目录、已安装插件动作和宿主操作。
func RegisterPlugins(g *gin.RouterGroup, dataDir string, db *gorm.DB, automation automationPluginControl,
	downloaders func(context.Context) ([]PluginDownloaderSummary, error), tjupt TjuptPluginServices,
	inventory *torrentdata.DownloaderInventory, inventoryList func(bool) []map[string]any) error {
	catalog := plugins.New(dataDir,
		plugins.Manifest{
			SchemaVersion: 2, ID: automationPluginID, Name: "自动转种", Version: "2.0",
			Description: "管理候选预览、转种规则、每月计划与任务记录。",
			Blocks:      []plugins.Block{},
		},
	)
	ctx := context.Background()
	packages, err := plugins.NewPackageManager(ctx, dataDir, automationPluginID)
	if err != nil {
		return err
	}
	h := &pluginHandler{
		catalog: catalog, packages: packages, db: db, automation: automation, downloaders: downloaders,
		inventory:        inventory,
		inventoryList:    inventoryList,
		tjupt:            tjupt.Feed,
		schedulesChanged: make(chan struct{}, 1),
	}
	h.tjupt.BindPluginAvailability(h.tjuptReady)
	if err := h.pauseUnavailableTjupt(ctx); err != nil {
		_ = packages.Close(ctx)
		return err
	}
	if err := h.startSchedules(ctx); err != nil {
		_ = packages.Close(ctx)
		return err
	}
	g.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	g.GET("", h.list)
	g.POST("/refresh", h.refresh)
	g.PUT("/:id/enabled", h.setEnabled)
	g.PUT("/:id/settings", h.saveSettings)
	g.GET("/:id/schedule", h.getSchedule)
	g.PUT("/:id/schedule", h.putSchedule)
	g.POST("/:id/actions/:action", h.invoke)
	g.GET("/downloader-summary/inventory", h.downloaderInventory)
	RegisterTjuptRSS(g.Group("/tjupt"), tjupt.RSS, tjupt.Automation, tjupt.Feed, &h.tjuptTransition, h.invokeTjupt)
	return nil
}

func (h *pluginHandler) list(c *gin.Context) {
	h.respond(c, h.catalog.List(), h.packages.List(), nil)
}

func (h *pluginHandler) refresh(c *gin.Context) {
	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()
	if !lockTjuptTransition(c, &h.tjuptTransition) {
		return
	}
	defer h.tjuptTransition.Unlock()
	ctx := c.Request.Context()
	if err := h.pauseUnavailableTjupt(ctx); err != nil {
		tjuptFeedError(c, err, http.StatusServiceUnavailable, "北洋园旧循环暂时无法暂停")
		return
	}
	legacy, legacyErr := h.catalog.Reload()
	installed, installErr := h.packages.Refresh(ctx)
	h.wakeSchedules()
	var issues []plugins.Issue
	if legacyErr != nil {
		issues = append(issues, plugins.Issue{File: "plugins/", Message: legacyErr.Error()})
	}
	if installErr != nil {
		issues = append(issues, plugins.Issue{File: "plugins/", Message: installErr.Error()})
	}
	if err := h.pauseUnavailableTjupt(ctx); err != nil {
		issues = append(issues, plugins.Issue{File: "tjupt-rss.zip", Message: "北洋园已阻止后续下载器操作，循环状态暂时无法保存"})
	}
	h.respond(c, legacy, installed, issues)
}

func (h *pluginHandler) respond(c *gin.Context, legacy plugins.Snapshot, installed plugins.PackageSnapshot, extra []plugins.Issue) {
	issues := append(append([]plugins.Issue{}, legacy.Issues...), installed.Issues...)
	issues = append(issues, extra...)
	packageIDs := make(map[string]bool, len(installed.Items))
	items := make([]any, 0, len(legacy.Items)+len(installed.Items))
	for _, item := range installed.Items {
		packageIDs[item.ID] = true
	}
	for _, item := range legacy.Items {
		if item.ID == plugins.TjuptFeedPluginID {
			issues = append(issues, plugins.Issue{File: item.ID + ".json", Message: "北洋园使用 tjupt-rss.zip 安装"})
			continue
		}
		if item.ID == automationPluginID {
			enabled, err := h.automation.PluginEnabled(c.Request.Context())
			if err != nil {
				issues = append(issues, plugins.Issue{File: item.ID, Message: "读取自动转种启用状态失败"})
			}
			items = append(items, automationPluginItem(item, enabled && err == nil))
		} else if packageIDs[item.ID] {
			issues = append(issues, plugins.Issue{File: item.ID + ".json", Message: "同 ID 的 ZIP 插件已加载"})
		} else {
			items = append(items, item)
		}
	}
	for _, item := range installed.Items {
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items, "issues": issues,
		"metrics": plugins.Counts(c.Request.Context(), h.db, legacy),
	})
}

func automationPluginItem(item plugins.Manifest, enabled bool) any {
	return struct {
		plugins.Manifest
		Kind    string `json:"kind"`
		Enabled bool   `json:"enabled"`
	}{item, "builtin", enabled}
}

func (h *pluginHandler) setEnabled(c *gin.Context) {
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if !bindPluginSettings(c, &payload) {
		return
	}
	if payload.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供启用状态"})
		return
	}
	id := c.Param("id")
	if id == plugins.TjuptFeedPluginID {
		h.setTjuptEnabled(c, *payload.Enabled)
		return
	}
	if id == automationPluginID {
		if err := h.automation.SetPluginEnabled(c.Request.Context(), *payload.Enabled); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "保存自动转种开关失败"})
			return
		}
		for _, manifest := range h.catalog.List().Items {
			if manifest.ID == id {
				c.JSON(http.StatusOK, gin.H{"plugin": automationPluginItem(manifest, *payload.Enabled)})
				return
			}
		}
	}
	plugin, err := h.packages.SetEnabled(id, *payload.Enabled)
	if err != nil {
		pluginError(c, err)
		return
	}
	h.wakeSchedules()
	c.JSON(http.StatusOK, gin.H{"plugin": plugin})
}

func (h *pluginHandler) saveSettings(c *gin.Context) {
	if c.Param("id") == plugins.TjuptFeedPluginID {
		pluginError(c, plugins.ErrPackageAction)
		return
	}
	var payload struct {
		Settings map[string]any `json:"settings"`
	}
	if !bindPluginSettings(c, &payload) {
		return
	}
	if payload.Settings == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供插件配置"})
		return
	}
	plugin, err := h.packages.SaveSettings(c.Param("id"), payload.Settings)
	if err != nil {
		pluginError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plugin": plugin})
}

func (h *pluginHandler) invoke(c *gin.Context) {
	if c.Param("id") == plugins.TjuptFeedPluginID {
		h.invokeTjupt(c, c.Param("action"))
		return
	}
	result, err := h.invokeAction(c.Request.Context(), c.Param("id"), c.Param("action"))
	if err != nil {
		pluginError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": result})
}

func (h *pluginHandler) invokeAction(ctx context.Context, id, action string) (plugins.ActionResult, error) {
	if id == plugins.TjuptFeedPluginID {
		return plugins.ActionResult{}, plugins.ErrPackageAction
	}
	info, err := h.packages.Get(id)
	if err != nil {
		return plugins.ActionResult{}, err
	}
	if !info.Enabled {
		return plugins.ActionResult{}, plugins.ErrPackageDisabled
	}
	resources := make(map[string]any)
	for _, capability := range info.Capabilities {
		if capability == plugins.DownloaderSummaryCapability {
			data, err := h.downloaders(ctx)
			if err != nil {
				return plugins.ActionResult{}, err
			}
			resources[capability] = data
		}
	}
	return h.packages.Invoke(ctx, id, action, resources)
}

func bindPluginSettings(c *gin.Context, payload any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if err := c.ShouldBindJSON(payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "插件配置格式错误"})
		return false
	}
	return true
}

func pluginError(c *gin.Context, err error) {
	status, message := http.StatusServiceUnavailable, "插件操作失败"
	switch {
	case errors.Is(err, plugins.ErrPackageNotFound):
		status, message = http.StatusNotFound, "未找到插件"
	case errors.Is(err, plugins.ErrPackageDisabled):
		status, message = http.StatusConflict, "插件已停用"
	case errors.Is(err, plugins.ErrPackageAction):
		status, message = http.StatusNotFound, "未找到插件操作"
	case errors.Is(err, plugins.ErrPackageSettings):
		status, message = http.StatusBadRequest, "插件配置无效"
	case errors.Is(err, plugins.ErrPackageExecution):
		status, message = http.StatusUnprocessableEntity, "插件运行失败，请检查插件代码或运行限制"
	}
	c.JSON(status, gin.H{"message": message})
}
