package settings

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/pt-nexus/server/internal/config"
)

func (h *Handler) GetPTGenSettings(c *gin.Context) {
	view, err := h.settings.GetPTGenSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}

func (h *Handler) SavePTGenSettings(c *gin.Context) {
	var input struct {
		Sources *[]config.PTGenSourceInput `json:"sources"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(&input); err != nil || input.Sources == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请求必须包含 PTGen 来源列表"})
		return
	}
	view, err := h.settings.SavePTGenSettings(*input.Sources)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, config.ErrPTGenSettingsUnavailable) || errors.Is(err, config.ErrPTGenSettingsWrite) {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"message": err.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, view)
}
