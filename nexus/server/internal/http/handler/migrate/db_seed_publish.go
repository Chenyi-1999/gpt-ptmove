package migrate

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
)

const (
	dbSeedInfoHandlerLogModule      = "迁移接口-种子查询"
	fetchStoreHandlerLogModule      = "迁移接口-源站抓取"
	updateDBSeedHandlerLogModule    = "迁移接口-种子更新"
	updateScreenshotReviewLogModule = "迁移接口-截图审查"
)

func (h *Handler) GetDBSeedInfo(c *gin.Context) {
	torrentID := strings.TrimSpace(c.Query("torrent_id"))
	siteName := strings.TrimSpace(c.Query("site_name"))
	taskID := strings.TrimSpace(c.Query("task_id"))
	requestID := c.GetString("request_id")
	logx.Infof(dbSeedInfoHandlerLogModule, "收到请求 请求ID=%s torrent_id=%s site_name=%s task_id=%s", requestID, torrentID, siteName, taskID)
	result, status := h.service.GetDBSeedInfo(torrentID, siteName, taskID)
	if success, ok := result["success"].(bool); ok && !success {
		logx.Warnf(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d success=%t error_code=%s", requestID, status, success, handlerToString(result["error_code"], ""))
	} else if status >= http.StatusBadRequest {
		logx.Warnf(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	} else {
		logx.Infof(dbSeedInfoHandlerLogModule, "请求结束 请求ID=%s 状态码=%d", requestID, status)
	}
	c.JSON(status, result)
}

// PrepareSeedInfo 在转种准备时读取已缓存资料，并补全缺失的 MediaInfo。
// 参数/返回：请求体包含 torrent_id、site_name 和可选 task_id；返回准备后的种子资料。
// 失败场景：缺少参数或数据库读取失败时返回相应错误；MediaInfo 补全失败时保留资料并附带诊断信息。
// 副作用：缓存命中且 MediaInfo 无效时可能读取下载器文件状态、请求飞牛媒体代理并更新 MediaInfo。
func (h *Handler) PrepareSeedInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	torrentID := strings.TrimSpace(handlerToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(handlerToString(payload["site_name"], ""))
	taskID := strings.TrimSpace(handlerToString(payload["task_id"], ""))
	logx.Infof(dbSeedInfoHandlerLogModule, "收到转种准备 请求ID=%s torrent_id=%s site_name=%s task_id=%s", requestID, torrentID, siteName, taskID)
	result, status := h.service.PrepareSeedInfo(torrentID, siteName, taskID)
	if status >= http.StatusBadRequest {
		logx.Warnf(dbSeedInfoHandlerLogModule, "转种准备读取结束 请求ID=%s 状态码=%d", requestID, status)
	} else {
		logx.Infof(dbSeedInfoHandlerLogModule, "转种准备读取结束 请求ID=%s 状态码=%d", requestID, status)
	}
	c.JSON(status, result)
}

func (h *Handler) FetchAndStore(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(fetchStoreHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	sourceSite := strings.TrimSpace(handlerToString(payload["sourceSite"], ""))
	searchTerm := strings.TrimSpace(handlerToString(payload["searchTerm"], ""))
	taskID := strings.TrimSpace(handlerToString(payload["task_id"], ""))
	logx.Infof(fetchStoreHandlerLogModule, "收到请求 请求ID=%s source_site=%s search_term=%s task_id=%s", requestID, sourceSite, searchTerm, taskID)
	result, status := h.service.FetchAndStore(payload)
	if success, ok := result["success"].(bool); ok && !success {
		logx.Warnf(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d success=%t error_code=%s", requestID, sourceSite, searchTerm, status, success, handlerToString(result["error_code"], ""))
	} else if status >= http.StatusBadRequest {
		logx.Warnf(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d", requestID, sourceSite, searchTerm, status)
	} else {
		logx.Infof(fetchStoreHandlerLogModule, "请求结束 请求ID=%s source_site=%s search_term=%s 状态码=%d", requestID, sourceSite, searchTerm, status)
	}
	c.JSON(status, result)
}

// RefetchMaintenanceSeed 拉取选定源站的资料并返回当前维护记录。
// 请求体无效时返回 400，业务层负责校验身份和保存范围。
func (h *Handler) RefetchMaintenanceSeed(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.RefetchMaintenanceSeed(payload)
	logx.Infof(dbSeedInfoHandlerLogModule, "维护记录重新拉取结束 请求ID=%s 状态码=%d", c.GetString("request_id"), status)
	c.JSON(status, result)
}

func (h *Handler) UpdateDBSeedInfo(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(updateDBSeedHandlerLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	torrentID := strings.TrimSpace(handlerToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(handlerToString(payload["site_name"], ""))
	logx.Infof(updateDBSeedHandlerLogModule, "收到请求 请求ID=%s torrent_id=%s site_name=%s", requestID, torrentID, siteName)
	result, status := h.service.UpdateDBSeedInfo(payload)
	if status >= http.StatusBadRequest {
		logx.Warnf(updateDBSeedHandlerLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	} else {
		logx.Infof(updateDBSeedHandlerLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	}
	c.JSON(status, result)
}

// UpdateScreenshotReviewStatus 更新指定种子的截图人工确认状态。
// 参数/返回：请求体包含 torrent_id、site_name 与 screenshot_review_status；成功时返回最新状态。
// 失败场景：请求体无效、参数缺失或数据库写入失败时返回对应错误码。
// 副作用：会更新 seed_parameters 的 screenshot_review_status 字段。
func (h *Handler) UpdateScreenshotReviewStatus(c *gin.Context) {
	requestID := c.GetString("request_id")
	payload, ok := bindMapPayload(c)
	if !ok {
		logx.Warnf(updateScreenshotReviewLogModule, "请求体绑定失败 请求ID=%s", requestID)
		return
	}
	torrentID := strings.TrimSpace(handlerToString(payload["torrent_id"], ""))
	siteName := strings.TrimSpace(handlerToString(payload["site_name"], ""))
	logx.Infof(updateScreenshotReviewLogModule, "收到请求 请求ID=%s torrent_id=%s site_name=%s", requestID, torrentID, siteName)
	result, status := h.service.UpdateScreenshotReviewStatus(payload)
	if status >= http.StatusBadRequest {
		logx.Warnf(updateScreenshotReviewLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	} else {
		logx.Infof(updateScreenshotReviewLogModule, "请求结束 请求ID=%s torrent_id=%s site_name=%s 状态码=%d", requestID, torrentID, siteName, status)
	}
	c.JSON(status, result)
}

func (h *Handler) Publish(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.Publish(payload)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchStart(c *gin.Context) {
	payload, ok := bindMapPayload(c)
	if !ok {
		return
	}
	result, status := h.service.StartPublishBatch(payload)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchStatus(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 batch_id 参数"})
		return
	}
	result, status := h.service.PublishBatchStatus(batchID)
	c.JSON(status, result)
}

func (h *Handler) PublishBatchCancel(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("batch_id"))
	if batchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 batch_id 参数"})
		return
	}
	result, status := h.service.PublishBatchCancel(batchID)
	c.JSON(status, result)
}
