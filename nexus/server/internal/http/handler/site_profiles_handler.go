package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
	"github.com/pt-nexus/server/internal/service/processing/siteprofile"
)

// SiteProfilesHandler 只负责公开声明与凭据请求解析；管理员鉴权由路由组强制提供。
type SiteProfilesHandler struct {
	service *migrationflow.SiteProfileService
}

// NewSiteProfilesHandler 创建站点资料 HTTP 适配器。
func NewSiteProfilesHandler(s *migrationflow.SiteProfileService) *SiteProfilesHandler {
	return &SiteProfilesHandler{s}
}

// RegisterRoutes 在已验证管理员 JWT 的 /api 组注册站点资料接口。
func (h *SiteProfilesHandler) RegisterRoutes(group *gin.RouterGroup) {
	g := group.Group("/site-profiles")
	g.GET("", h.List)
	g.GET("/schema", h.Schema)
	g.GET("/template", h.Template)
	g.POST("/preview", h.Preview)
	g.POST("/import", h.Import)
	g.GET("/:id/export", h.Export)
	g.GET("/:id/credentials", h.Credentials)
	g.PUT("/:id/credentials", h.SetCredentials)
}

// List 返回原生和导入站点的公开资料。
func (h *SiteProfilesHandler) List(c *gin.Context) {
	rows, e := h.service.List()
	if e != nil {
		c.JSON(500, gin.H{"error": "cannot list site profiles"})
		return
	}
	c.JSON(200, gin.H{"sites": rows})
}

// Schema 返回声明式资料的公开 JSON Schema。
func (h *SiteProfilesHandler) Schema(c *gin.Context) { c.JSON(200, SiteProfileSchema()) }

// Template 返回兼容 0.2 格式的可编辑声明。
func (h *SiteProfilesHandler) Template(c *gin.Context) {
	p := siteprofile.Default()
	p.ID = "example"
	p.Name = "Example"
	p.BaseURL = "https://tracker.example.com"
	c.JSON(200, p)
}
func readProfileBody(c *gin.Context) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
}

// Preview 离线检查资料，不添加原生站点或变更凭据。
func (h *SiteProfilesHandler) Preview(c *gin.Context) {
	raw, e := readProfileBody(c)
	if e != nil {
		c.JSON(400, gin.H{"error": "profile exceeds 64KiB"})
		return
	}
	p, e := h.service.Preview(raw)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"profile": p})
}

type profileImportRequest struct {
	Profile          json.RawMessage `json:"profile"`
	Update           bool            `json:"update"`
	ExpectedRevision int             `json:"expected_revision"`
}

// Import 显式创建或按版本更新资料与原生站点。
func (h *SiteProfilesHandler) Import(c *gin.Context) {
	var req profileImportRequest
	if e := decodeProfileJSON(c, 67584, &req); e != nil {
		c.JSON(400, gin.H{"error": "invalid import request"})
		return
	}
	p, e := h.service.Import(req.Profile, req.Update, req.ExpectedRevision)
	if e != nil {
		c.JSON(409, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"profile": p})
}

// Export 只返回可再次导入的公开声明。
func (h *SiteProfilesHandler) Export(c *gin.Context) {
	p, e := h.service.Export(c.Param("id"))
	if e != nil {
		c.JSON(404, gin.H{"error": "site not found"})
		return
	}
	c.JSON(200, p)
}

type profileCredentialsRequest struct {
	Cookie *string `json:"cookie"`
	RSS    *string `json:"rss"`
}

// Credentials 只公开是否已配置凭据，不返回凭据明文。
func (h *SiteProfilesHandler) Credentials(c *gin.Context) {
	cookie, e := h.service.Cookie(c.Param("id"))
	if e != nil {
		c.JSON(404, gin.H{"error": "site not found"})
		return
	}
	rss, e := h.service.RSS(c.Param("id"))
	if e != nil {
		c.JSON(500, gin.H{"error": "cannot read credential status"})
		return
	}
	c.JSON(200, gin.H{"has_cookie": cookie != "", "has_rss": rss != "", "cookie_storage": "native-compatible", "rss_storage": "encrypted"})
}

// SetCredentials 省略字段保留，空值清除；不回显输入。
func (h *SiteProfilesHandler) SetCredentials(c *gin.Context) {
	var req profileCredentialsRequest
	if decodeProfileJSON(c, 40000, &req) != nil {
		c.JSON(400, gin.H{"error": "invalid credentials request"})
		return
	}
	if e := h.service.SetCredentials(c.Param("id"), req.Cookie, req.RSS); e != nil {
		c.JSON(400, gin.H{"error": "credential validation or storage failed"})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// SiteProfileSchema 精确声明公开可导入字段；附加字段一律禁止。
func SiteProfileSchema() map[string]any {
	str := func(max int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
	}
	paths := map[string]any{}
	for _, k := range []string{"listing", "detail", "download", "search"} {
		paths[k] = str(1024)
	}
	selectors := map[string]any{}
	for _, k := range []string{"table", "detail_link", "title", "description", "download_link", "tags", "peers", "restriction_allowed", "restriction_denied", "search_empty", "search_results", "seeders", "official", "leechers"} {
		selectors[k] = str(512)
	}
	object := func(properties map[string]any) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	}
	props := map[string]any{"schema_version": map[string]any{"type": "integer", "const": 1}, "id": map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9_-]{0,63}$"}, "name": str(100), "base_url": map[string]any{"type": "string", "format": "uri", "maxLength": 512, "pattern": "^https://"}, "engine": map[string]any{"type": "string", "const": "nexusphp"}, "ptnexus_site": map[string]any{"type": []string{"string", "null"}, "pattern": "^[a-z][a-z0-9_-]{0,63}$"}, "roles": map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "uniqueItems": true, "items": map[string]any{"enum": []string{"source", "target"}}}, "official_groups": map[string]any{"type": "array", "maxItems": 64, "items": str(100)}, "announce_hosts": map[string]any{"type": "array", "maxItems": 32, "items": str(253)}, "paths": object(paths), "selectors": object(selectors)}
	schema := object(props)
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = "STARPT declarative site profile"
	schema["required"] = []string{"id", "name", "base_url", "roles"}
	return schema
}

func decodeProfileJSON(c *gin.Context, limit int64, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		if e != nil {
			return e
		}
		return errors.New("trailing JSON value")
	}
	return nil
}
