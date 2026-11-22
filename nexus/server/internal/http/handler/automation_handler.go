package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

// AutomationHandler 仅解析有界请求并调用自动化编排服务；路由由原生严格登录保护注册。
type AutomationHandler struct {
	service *migrationflow.AutomationService
}

// NewAutomationHandler 创建自动化接口适配器，不启动线程。
func NewAutomationHandler(service *migrationflow.AutomationService) *AutomationHandler {
	return &AutomationHandler{service: service}
}

// RegisterRoutes 注册模块管理、只读预览和显式任务操作。
func (h *AutomationHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/settings", h.GetSettings)
	g.PUT("/settings", h.UpdateSettings)
	g.POST("/preview", h.Preview)
	g.POST("/runs", h.Run)
	g.GET("/runs", h.ListRuns)
	g.GET("/tasks", h.ListTasks)
	g.POST("/tasks/:id/cancel", h.Cancel)
	g.POST("/tasks/:id/retry", h.Retry)
	g.POST("/tasks/:id/reconcile", h.Reconcile)
}
func automationBody(c *gin.Context) (map[string]any, bool) {
	body := map[string]any{}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	if err := decoder.Decode(&body); err != nil && err != io.EOF {
		c.JSON(400, gin.H{"success": false, "message": "请求应为 64KiB 内的 JSON 对象"})
		return nil, false
	}
	if body == nil {
		c.JSON(400, gin.H{"success": false, "message": "请求应为 JSON 对象"})
		return nil, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(400, gin.H{"success": false, "message": "请求含多余 JSON 内容"})
		return nil, false
	}
	return body, true
}

// GetSettings 读取公开设置和真实月度用量。
func (h *AutomationHandler) GetSettings(c *gin.Context) { c.JSON(200, h.service.GetSettings()) }

// UpdateSettings 保存白名单配置，保持执行与发布独立开关。
func (h *AutomationHandler) UpdateSettings(c *gin.Context) {
	body, ok := automationBody(c)
	if !ok {
		return
	}
	result, status := h.service.UpdateSettingsContext(c.Request.Context(), body)
	c.JSON(status, result)
}

// Preview 只读取候选，不产生下载、队列或配额副作用。
func (h *AutomationHandler) Preview(c *gin.Context) {
	body, ok := automationBody(c)
	if !ok {
		return
	}
	result, status := h.service.Preview(c.Request.Context(), body)
	c.JSON(status, result)
}

// Run 按当前资料和规则显式预留任务。
func (h *AutomationHandler) Run(c *gin.Context) {
	body, ok := automationBody(c)
	if !ok {
		return
	}
	result, status := h.service.Run(c.Request.Context(), body)
	c.JSON(status, result)
}

// ListRuns 返回持久扫描记录。
func (h *AutomationHandler) ListRuns(c *gin.Context) {
	result, status := h.service.ListRuns()
	c.JSON(status, result)
}

// ListTasks 返回真实任务和保守配额。
func (h *AutomationHandler) ListTasks(c *gin.Context) {
	if c.Query("active") == "1" {
		result, status := h.service.ListActiveTasks(c.Request.Context())
		c.JSON(status, result)
		return
	}
	result, status := h.service.ListTasksContext(c.Request.Context())
	c.JSON(status, result)
}

// Cancel 停止任务，不暂停或删除下载器已有种子。
func (h *AutomationHandler) Cancel(c *gin.Context) {
	result, status := h.service.CancelContext(c.Request.Context(), c.Param("id"))
	c.JSON(status, result)
}

// Retry 只恢复尚未尝试上传的失败任务。
func (h *AutomationHandler) Retry(c *gin.Context) {
	result, status := h.service.RetryContext(c.Request.Context(), c.Param("id"))
	c.JSON(status, result)
}

// Reconcile 核对目标详情及同载荷做种证据，绝不重新上传。
func (h *AutomationHandler) Reconcile(c *gin.Context) {
	body, ok := automationBody(c)
	if !ok {
		return
	}
	url, _ := body["url"].(string)
	if url == "" {
		c.JSON(400, gin.H{"success": false, "message": "请提供目标种子详情地址"})
		return
	}
	result, status := h.service.Reconcile(c.Request.Context(), c.Param("id"), url)
	c.JSON(status, result)
}
