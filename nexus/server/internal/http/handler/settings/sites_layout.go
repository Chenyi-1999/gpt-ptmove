package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"
	settingsservice "github.com/pt-nexus/server/internal/service/settings"
)

// GetSitesLayoutSettings 返回做种检索页面布局。
// 参数/返回：从 HTTP 请求读取用户身份上下文；成功时直接返回布局 JSON。
// 失败场景：无，配置异常时服务层返回默认布局。
// 副作用：无副作用，仅读取设置。
func (h *Handler) GetSitesLayoutSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetSitesLayoutSettings())
}

// SaveSitesLayoutSettings 校验并保存做种检索页面布局。
// 参数/返回：请求体包含 columns 和 order；成功返回 success=true。
// 失败场景：请求体格式或列数不合法时返回 400；配置持久化失败时返回 500。
// 副作用：写入 UI 布局配置文件。
func (h *Handler) SaveSitesLayoutSettings(c *gin.Context) {
	var payload settingsservice.SitesLayoutSettings
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Order == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "布局配置格式错误"})
		return
	}
	if payload.Columns != 1 && payload.Columns != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "列数必须为 1 或 2"})
		return
	}
	if err := h.settings.SaveSitesLayoutSettings(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "无法保存做种检索布局"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
