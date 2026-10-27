package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

// AutomationPort 是机器 API 调用的自动化业务端口，不转发到管理员 HTTP 接口。
type AutomationPort interface {
	GetSettings() map[string]any
	UpdateSettingsContext(context.Context, map[string]any) (map[string]any, int)
	Preview(context.Context, map[string]any) (map[string]any, int)
	Run(context.Context, map[string]any) (map[string]any, int)
	ListTasks() (map[string]any, int)
	ListRuns() (map[string]any, int)
	Cancel(string) (map[string]any, int)
	Retry(string) (map[string]any, int)
	Reconcile(context.Context, string, string) (map[string]any, int)
}

// MachineHandler 适配独立 Bearer API；管理员密钥管理必须由 JWT 路由组保护。
type MachineHandler struct {
	keys       *migrationflow.MachineAPIService
	profiles   *migrationflow.SiteProfileService
	automation AutomationPort
}

// NewMachineHandler 将站点和自动化服务组合成有范围限制的 API。
func NewMachineHandler(k *migrationflow.MachineAPIService, p *migrationflow.SiteProfileService, a AutomationPort) *MachineHandler {
	return &MachineHandler{k, p, a}
}

// RegisterAdmin 只在实际管理员 JWT 组提供创建、列举和撤销机器密钥。
func (h *MachineHandler) RegisterAdmin(g *gin.RouterGroup) {
	g.GET("/machine-openapi", h.OpenAPI)
	g.GET("/machine-keys", h.ListKeys)
	g.POST("/machine-keys", h.CreateKey)
	g.DELETE("/machine-keys/:id", h.RevokeKey)
}

// RegisterV1 为 /api/v1 注册自己的 Bearer 守卫，没有其他认证回退。
func (h *MachineHandler) RegisterV1(g *gin.RouterGroup) {
	g.Use(h.authenticate)
	g.GET("/openapi.json", h.OpenAPI)
	g.GET("/dashboard", h.Dashboard)
	g.GET("/rules", h.Rules)
	g.GET("/sites", h.Sites)
	g.GET("/sites/schema", func(c *gin.Context) { c.JSON(200, SiteProfileSchema()) })
	g.GET("/sites/template", func(c *gin.Context) { NewSiteProfilesHandler(h.profiles).Template(c) })
	g.GET("/sites/:id/export", func(c *gin.Context) { NewSiteProfilesHandler(h.profiles).Export(c) })
	g.GET("/tasks", h.Tasks)
	g.GET("/runs", h.Runs)
	g.PUT("/rules", h.UpdateRules)
	g.POST("/sites/preview", h.PreviewSite)
	g.POST("/sites/import", h.ImportSite)
	g.PUT("/sites/:id/credentials", h.SiteCredentials)
	g.POST("/scans", h.Scan)
	g.POST("/runs", h.Run)
	g.POST("/tasks", h.Run)
	g.POST("/tasks/:id/cancel", h.Cancel)
	g.POST("/tasks/:id/retry", h.Retry)
	g.POST("/tasks/:id/reconcile", h.Reconcile)
}
func (h *MachineHandler) authenticate(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || strings.Count(header, " ") != 1 {
		c.AbortWithStatusJSON(401, gin.H{"error": "machine Bearer key required"})
		return
	}
	p, e := h.keys.Authenticate(strings.TrimPrefix(header, "Bearer "))
	if e != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "invalid machine key"})
		return
	}
	if !p.Allows("read") {
		c.AbortWithStatusJSON(403, gin.H{"error": "read scope required"})
		return
	}
	c.Set("machine_principal", p)
	c.Next()
}
func machinePrincipal(c *gin.Context) *migrationflow.MachinePrincipal {
	return c.MustGet("machine_principal").(*migrationflow.MachinePrincipal)
}

// ListKeys 返回机器密钥公开元数据。
func (h *MachineHandler) ListKeys(c *gin.Context) {
	keys, e := h.keys.ListKeys()
	if e != nil {
		c.JSON(500, gin.H{"error": "cannot list keys"})
		return
	}
	c.JSON(200, gin.H{"keys": keys})
}

// CreateKey 创建一次显示的机器密钥；到期日必须明确给出。
func (h *MachineHandler) CreateKey(c *gin.Context) {
	var body struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresAt string   `json:"expires_at"`
	}
	if decodeProfileJSON(c, 8192, &body) != nil {
		c.JSON(400, gin.H{"error": "invalid key request"})
		return
	}
	expiry, e := time.Parse(time.RFC3339, body.ExpiresAt)
	if e != nil {
		c.JSON(400, gin.H{"error": "expires_at must be RFC3339"})
		return
	}
	out, e := h.keys.CreateKey(body.Name, body.Scopes, expiry)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(201, out)
}

// RevokeKey 撤销指定机器密钥。
func (h *MachineHandler) RevokeKey(c *gin.Context) {
	if h.keys.RevokeKey(c.Param("id")) != nil {
		c.JSON(404, gin.H{"error": "key not found"})
		return
	}
	c.JSON(200, gin.H{"success": true})
}
func safeMachineValue(v any) any {
	b, e := json.Marshal(v)
	if e != nil {
		return map[string]any{"error": "invalid service response"}
	}
	var out any
	json.Unmarshal(b, &out)
	return redactMachineValue(out)
}
func redactMachineValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			low := strings.ToLower(k)
			if strings.Contains(low, "cookie") || strings.Contains(low, "passkey") || strings.Contains(low, "password") || strings.Contains(low, "secret") || strings.Contains(low, "token") || low == "rss" || low == "api_key" || low == "credentials" || low == "snapshot" {
				continue
			}
			out[k] = redactMachineValue(x)
		}
		return out
	case []any:
		for i := range t {
			t[i] = redactMachineValue(t[i])
		}
		return t
	default:
		return v
	}
}

// Dashboard 只返回自动化状态与公开历史。
func (h *MachineHandler) Dashboard(c *gin.Context) {
	tasks, ts := h.automation.ListTasks()
	if ts >= 400 {
		c.JSON(ts, safeMachineValue(tasks))
		return
	}
	runs, rs := h.automation.ListRuns()
	if rs >= 400 {
		c.JSON(rs, safeMachineValue(runs))
		return
	}
	c.JSON(200, safeMachineValue(gin.H{"rules": h.automation.GetSettings(), "tasks": tasks, "runs": runs}))
}

// Rules 返回独立自动化规则，不暴露全局配置。
func (h *MachineHandler) Rules(c *gin.Context) {
	c.JSON(200, safeMachineValue(h.automation.GetSettings()))
}

// Sites 返回公开站点声明。
func (h *MachineHandler) Sites(c *gin.Context) { NewSiteProfilesHandler(h.profiles).List(c) }

// Tasks 返回实际持久任务，不插入演示记录。
func (h *MachineHandler) Tasks(c *gin.Context) {
	out, status := h.automation.ListTasks()
	c.JSON(status, safeMachineValue(out))
}

// Runs 返回实际扫描记录。
func (h *MachineHandler) Runs(c *gin.Context) {
	out, status := h.automation.ListRuns()
	c.JSON(status, safeMachineValue(out))
}
func (h *MachineHandler) guardedWriteScopeContext(ctx context.Context) string {
	settingsPort, ok := h.automation.(interface {
		GetSettingsContext(context.Context) map[string]any
	})
	if !ok {
		return "write+publish"
	}
	tasksPort, ok := h.automation.(interface {
		ListTasksContext(context.Context) (map[string]any, int)
	})
	if !ok {
		return "write+publish"
	}
	settings := settingsPort.GetSettingsContext(ctx)
	if success, ok := settings["success"].(bool); ok && !success {
		return "write+publish"
	}
	if recursiveEnabled(safeMachineValue(settings)) {
		return "write+publish"
	}
	tasks, status := tasksPort.ListTasksContext(ctx)
	if status >= 400 {
		return "write+publish"
	}
	b, _ := json.Marshal(tasks)
	var values map[string]any
	json.Unmarshal(b, &values)
	if hasPending(values) {
		return "write+publish"
	}
	return "write"
}
func recursiveEnabled(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "auto_publish" || k == "enabled" {
				if yes, ok := v.(bool); ok && yes {
					return true
				}
			}
			if recursiveEnabled(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if recursiveEnabled(v) {
				return true
			}
		}
	}
	return false
}
func hasPending(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if state, ok := x["state"].(string); ok {
			switch state {
			case "cancelled", "failed", "succeeded", "seeding":
			default:
				return true
			}
		}
		for _, v := range x {
			if hasPending(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if hasPending(v) {
				return true
			}
		}
	}
	return false
}

type machineCommand func(context.Context, map[string]any, []byte) (map[string]any, int)

func (h *MachineHandler) command(c *gin.Context, scope string, run machineCommand) {
	h.commandScope(c, func(context.Context, map[string]any) string { return scope }, run)
}
func (h *MachineHandler) commandScope(c *gin.Context, determineScope func(context.Context, map[string]any) string, run machineCommand) {
	h.commandMode(c, determineScope, run, false)
}
func (h *MachineHandler) commandMode(c *gin.Context, determineScope func(context.Context, map[string]any) string, run machineCommand, readOnly bool) {
	raw, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 67584))
	if e != nil {
		c.JSON(400, gin.H{"error": "request too large"})
		return
	}
	body := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &body) != nil {
		c.JSON(400, gin.H{"error": "JSON object required"})
		return
	}
	if body == nil {
		c.JSON(400, gin.H{"error": "JSON object required"})
		return
	}
	if readOnly {
		out, status := h.keys.ExecuteReadCommand(c.Request.Context(), machinePrincipal(c), c.GetHeader("Idempotency-Key"), c.Request.Method, c.Request.URL.EscapedPath(), raw, determineScope(c.Request.Context(), body), func(ctx context.Context) (map[string]any, int) { return run(ctx, body, raw) })
		c.JSON(status, safeMachineValue(out))
		return
	}
	out, status := h.keys.ExecuteWithPolicy(c.Request.Context(), machinePrincipal(c), c.GetHeader("Idempotency-Key"), c.Request.Method, c.Request.URL.EscapedPath(), raw, func(ctx context.Context) string { return determineScope(ctx, body) }, func(ctx context.Context) (map[string]any, int) { return run(ctx, body, raw) })
	c.JSON(status, safeMachineValue(out))
}

// UpdateRules 原子更新规则；启用发布或已有待执行任务时要求发布权限。
func (h *MachineHandler) UpdateRules(c *gin.Context) {
	h.commandScope(c, func(ctx context.Context, body map[string]any) string {
		if recursiveEnabled(body) {
			return "write+publish"
		}
		return h.guardedWriteScopeContext(ctx)
	}, func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		if recursiveEnabled(body) && !machinePrincipal(c).Allows("publish") {
			return map[string]any{"error": "publish scope required"}, 403
		}
		return h.automation.UpdateSettingsContext(ctx, body)
	})
}

// PreviewSite 执行离线预览；为统一命令协议要求幂等键。
func (h *MachineHandler) PreviewSite(c *gin.Context) {
	h.command(c, "write", func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		p, e := h.profiles.PreviewContext(ctx, raw)
		if e != nil {
			return map[string]any{"error": e.Error()}, 400
		}
		return map[string]any{"profile": p}, 200
	})
}

// ImportSite 在业务事务内创建或显式更新站点。
func (h *MachineHandler) ImportSite(c *gin.Context) {
	h.commandScope(c, func(ctx context.Context, body map[string]any) string { return h.guardedWriteScopeContext(ctx) }, func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		for key := range body {
			if key != "profile" && key != "update" && key != "expected_revision" {
				return map[string]any{"error": "unknown import field"}, 400
			}
		}
		var req profileImportRequest
		if json.Unmarshal(raw, &req) != nil {
			return map[string]any{"error": "invalid import request"}, 400
		}
		p, e := h.profiles.ImportContext(ctx, req.Profile, req.Update, req.ExpectedRevision)
		if e != nil {
			return map[string]any{"error": e.Error()}, 409
		}
		return map[string]any{"profile": p}, 200
	})
}

// SiteCredentials 写入凭据，不返回凭据原文。
func (h *MachineHandler) SiteCredentials(c *gin.Context) {
	h.commandScope(c, func(ctx context.Context, body map[string]any) string { return h.guardedWriteScopeContext(ctx) }, func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		for key := range body {
			if key != "cookie" && key != "rss" {
				return map[string]any{"error": "unknown credential field"}, 400
			}
		}
		var req profileCredentialsRequest
		if json.Unmarshal(raw, &req) != nil {
			return map[string]any{"error": "invalid credential request"}, 400
		}
		if h.profiles.SetCredentialsContext(ctx, c.Param("id"), req.Cookie, req.RSS) != nil {
			return map[string]any{"error": "credential validation or storage failed"}, 400
		}
		return map[string]any{"success": true}, 200
	})
}

// Scan 扫描预览不预留配额，也不加入下载或发布队列。
func (h *MachineHandler) Scan(c *gin.Context) {
	h.commandMode(c, func(context.Context, map[string]any) string { return "write" }, func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		return h.automation.Preview(ctx, body)
	}, true)
}

// Run 创建真实自动化运行；必须有发布权限。
func (h *MachineHandler) Run(c *gin.Context) {
	h.command(c, "publish", func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		for key := range body {
			if key != "selected_ids" && key != "enqueue" {
				return map[string]any{"error": "unknown run field"}, 400
			}
		}
		if ids, ok := body["selected_ids"]; ok {
			values, ok := ids.([]any)
			if !ok {
				return map[string]any{"error": "selected_ids must be an array"}, 400
			}
			for _, v := range values {
				if _, ok := v.(string); !ok {
					return map[string]any{"error": "selected_ids must contain strings"}, 400
				}
			}
		}
		if enqueue, ok := body["enqueue"]; ok {
			if _, ok := enqueue.(bool); !ok {
				return map[string]any{"error": "enqueue must be boolean"}, 400
			}
		}
		return h.automation.Run(ctx, body)
	})
}

// Cancel 撤销后续执行授权，不新增发布。
func (h *MachineHandler) Cancel(c *gin.Context) {
	h.command(c, "write", func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		if len(body) != 0 {
			return map[string]any{"error": "cancel body must be empty"}, 400
		}
		if port, ok := h.automation.(interface {
			CancelContext(context.Context, string) (map[string]any, int)
		}); ok {
			return port.CancelContext(ctx, c.Param("id"))
		}
		return map[string]any{"error": "transactional cancellation unavailable"}, 503
	})
}

// Retry 仅调用具有事务上下文的重试业务，禁止直接重发上传。
func (h *MachineHandler) Retry(c *gin.Context) {
	h.command(c, "publish", func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		if len(body) != 0 {
			return map[string]any{"error": "retry body must be empty"}, 400
		}
		if port, ok := h.automation.(interface {
			RetryContext(context.Context, string) (map[string]any, int)
		}); ok {
			return port.RetryContext(ctx, c.Param("id"))
		}
		return map[string]any{"error": "transactional retry unavailable"}, 503
	})
}

// Reconcile 只读核对模糊发布结果，禁止重传种子。
func (h *MachineHandler) Reconcile(c *gin.Context) {
	h.command(c, "publish", func(ctx context.Context, body map[string]any, raw []byte) (map[string]any, int) {
		if len(body) != 1 {
			return map[string]any{"error": "only target_url is accepted"}, 400
		}
		targetURL, _ := body["target_url"].(string)
		if targetURL == "" {
			return map[string]any{"error": "target_url required"}, 400
		}
		return h.automation.Reconcile(ctx, c.Param("id"), targetURL)
	})
}
