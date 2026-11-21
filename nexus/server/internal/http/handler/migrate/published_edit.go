package migrate

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

// OpenPublishedEdit 返回当前站上资料及可用版本。
func (h *Handler) OpenPublishedEdit(c *gin.Context) {
	id, ok := publishedEditLogID(c)
	if !ok {
		return
	}
	result, status := h.service.OpenPublishedEdit(c.Request.Context(), id)
	c.JSON(status, result)
}

// GeneratePublishedEditMedia 发起飞牛截图或 MediaInfo 提取请求。
func (h *Handler) GeneratePublishedEditMedia(c *gin.Context) {
	id, ok := publishedEditLogID(c)
	var body migrationflow.PublishedEditMediaRequest
	if !ok || !bindPublishedEdit(c, &body) {
		return
	}
	result, status := h.service.GeneratePublishedEditMedia(c.Request.Context(), id, body)
	c.JSON(status, result)
}

// PreviewPublishedEdit 返回将要修改的站点字段差异。
func (h *Handler) PreviewPublishedEdit(c *gin.Context) {
	id, ok := publishedEditLogID(c)
	var body migrationflow.PublishedEditDraft
	if !ok || !bindPublishedEdit(c, &body) {
		return
	}
	result, status := h.service.PreviewPublishedEdit(c.Request.Context(), id, body)
	c.JSON(status, result)
}

// SavePublishedEdit 执行已明确确认的一次站点编辑。
func (h *Handler) SavePublishedEdit(c *gin.Context) {
	id, ok := publishedEditLogID(c)
	var body migrationflow.PublishedEditDraft
	if !ok || !bindPublishedEdit(c, &body) {
		return
	}
	result, status := h.service.SavePublishedEdit(c.Request.Context(), id, body)
	c.JSON(status, result)
}

func publishedEditLogID(c *gin.Context) (uint64, bool) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "任务记录 ID 无效"})
		return 0, false
	}
	return id, true
}

func bindPublishedEdit(c *gin.Context, body any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := c.ShouldBindJSON(body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供有效的编辑请求数据"})
		return false
	}
	return true
}
