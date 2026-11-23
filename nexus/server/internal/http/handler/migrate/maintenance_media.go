package migrate

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/migrationflow"
)

// StartMaintenanceMediaFill 校验选中的维护记录标识并登记后台补全任务。
func (h *Handler) StartMaintenanceMediaFill(c *gin.Context) {
	var payload struct {
		Seeds []migrationflow.MaintenanceMediaSeed `json:"seeds"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无法读取选中的种子，请刷新列表后重试"})
		return
	}
	result, status := h.service.StartMaintenanceMediaFill(payload.Seeds)
	c.JSON(status, result)
}
