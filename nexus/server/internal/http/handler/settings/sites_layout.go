package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"
	settingsservice "github.com/pt-nexus/server/internal/service/settings"
)

// GetHomeLayoutSettings 返回首页布局。配置缺失时返回默认值，无副作用。
func (h *Handler) GetHomeLayoutSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetHomeLayoutSettings())
}

// SaveHomeLayoutSettings 校验并持久化首页布局，返回保存结果。
func (h *Handler) SaveHomeLayoutSettings(c *gin.Context) {
	saveDashboardLayout(c, h.settings.SaveHomeLayoutSettings, "无法保存首页布局")
}

// GetSitesLayoutSettings 返回做种检索布局。配置缺失时返回默认值，无副作用。
func (h *Handler) GetSitesLayoutSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.GetSitesLayoutSettings())
}

// SaveSitesLayoutSettings 校验并持久化做种检索布局，返回保存结果。
func (h *Handler) SaveSitesLayoutSettings(c *gin.Context) {
	saveDashboardLayout(c, h.settings.SaveSitesLayoutSettings, "无法保存做种检索布局")
}

// saveDashboardLayout 读取列数、模块顺序和可选的隐藏列表，保存失败时返回 400 或 500。
func saveDashboardLayout(c *gin.Context, save func(settingsservice.DashboardLayoutSettings) error, failureMessage string) {
	var payload settingsservice.DashboardLayoutSettings
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Order == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "布局配置格式错误"})
		return
	}
	if payload.Columns != 1 && payload.Columns != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "列数必须为 1 或 2"})
		return
	}
	if err := save(payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": failureMessage})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
