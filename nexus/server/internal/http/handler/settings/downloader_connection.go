package settings

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/platform/logx"
	"github.com/pt-nexus/server/internal/service/downloaderclient"
)

const downloaderInfoModule = "设置-下载器"

// DownloadersList 返回启用下载器的简要列表。
// 参数/返回：无请求体，返回仅包含 id/name 的数组。
// 失败场景：读取配置异常时返回空数组（由服务层兜底）。
// 副作用：无副作用，仅查询内存配置。
func (h *Handler) DownloadersList(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.DownloadersList(true))
}

// AllDownloaders 返回全部下载器（含启用状态）的简要列表。
// 参数/返回：无请求体，返回包含 id/name/enabled 的数组。
// 失败场景：读取配置异常时返回空数组（由服务层兜底）。
// 副作用：无副作用，仅查询内存配置。
func (h *Handler) AllDownloaders(c *gin.Context) {
	c.JSON(http.StatusOK, h.settings.DownloadersList(false))
}

// TestConnection 测试与单个下载器的连接。
// 参数/返回：读取 JSON 配置并返回 success/message。
// 失败场景：参数非法、认证失败、API 不可达或代理不可达时返回 success=false。
// 副作用：查询下载器 API 的版本，启用远程媒体时验证媒体服务。
func (h *Handler) TestConnection(c *gin.Context) {
	payload := map[string]any{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求体格式错误"})
		return
	}

	name := handlerToString(payload["name"], "下载器")
	typ := strings.ToLower(strings.TrimSpace(handlerToString(payload["type"], "")))
	host := strings.TrimSpace(handlerToString(payload["host"], ""))
	username := handlerToString(payload["username"], "")
	password := handlerToString(payload["password"], "")
	apiKey := strings.TrimSpace(handlerToString(payload["api_key"], ""))
	if apiKey == "" && !handlerToBool(payload["clear_api_key"], false) {
		for _, saved := range h.settings.DownloaderConfigs() {
			if handlerToString(saved["id"], "") == handlerToString(payload["id"], "") {
				apiKey = handlerToString(saved["api_key"], "")
				if password == "" {
					password = handlerToString(saved["password"], "")
				}
				break
			}
		}
	}
	proxyToken := strings.TrimSpace(handlerToString(payload["proxy_token"], ""))
	if proxyToken == "" && !handlerToBool(payload["clear_proxy_token"], false) {
		for _, saved := range h.settings.DownloaderConfigs() {
			if handlerToString(saved["id"], "") == handlerToString(payload["id"], "") {
				proxyToken = handlerToString(saved["proxy_token"], "")
				break
			}
		}
	}
	useProxy := handlerToBool(payload["use_proxy"], false)
	proxyPort := handlerToInt(payload["proxy_port"], 9090)

	if typ != "qbittorrent" && typ != "transmission" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的客户端类型。"})
		return
	}
	if host == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("'%s' 连接失败: 缺少 host 配置", name)})
		return
	}

	downloader := downloaderclient.Downloader{
		ID:         handlerToString(payload["id"], ""),
		Name:       name,
		Type:       typ,
		Host:       strings.TrimSpace(host),
		Username:   username,
		Password:   password,
		APIKey:     apiKey,
		Enabled:    true,
		UseProxy:   useProxy,
		ProxyPort:  proxyPort,
		ProxyToken: proxyToken,
	}

	if _, err := downloader.FetchVersion(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": formatDownloaderConnectError(name, err, false)})
		return
	}
	if useProxy {
		if err := downloader.ProbeMediaProxy(); err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": formatDownloaderConnectError(name, err, true)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("下载器 '%s' 与媒体代理连接测试成功", name)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("下载器 '%s' 连接测试成功", name)})
}

func formatDownloaderConnectError(name string, err error, isProxy bool) string {
	if err == nil {
		return fmt.Sprintf("'%s' 连接失败: 未知错误", name)
	}
	errMsg := strings.TrimSpace(err.Error())
	lowered := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lowered, "401") || strings.Contains(lowered, "unauthorized"):
		errMsg = "认证失败，请检查下载器 API Token 或用户名和密码。"
	case strings.Contains(lowered, "403"):
		errMsg = "禁止访问，请检查权限设置。"
	}
	prefix := "连接失败"
	if isProxy {
		prefix = "媒体代理连接失败"
		if strings.Contains(lowered, "401") || strings.Contains(lowered, "403") || strings.Contains(lowered, "unauthorized") {
			errMsg = "认证失败，请检查媒体代理令牌。"
		}
	}
	return fmt.Sprintf("'%s' %s: %s", name, prefix, errMsg)
}

// DownloaderInfo 返回首页下载器状态与流量统计，语义对齐 Python 版本。
// 参数/返回：无请求体，返回每个下载器的状态、版本和今日/累计流量信息。
// 失败场景：DB 查询失败时流量字段回退为 0；连接探测失败时状态为“连接失败”并附错误信息。
// 副作用：查询启用下载器的 API 版本。
func (h *Handler) DownloaderInfo(c *gin.Context) {
	totals, err := h.torrents.TrafficTotals()
	if err != nil {
		totals = map[string]map[string]int64{}
		logx.Warnf(downloaderInfoModule, "读取累计流量失败 err=%v", err)
	}
	today, err := h.torrents.TrafficToday()
	if err != nil {
		today = map[string]map[string]int64{}
		logx.Warnf(downloaderInfoModule, "读取今日流量失败 err=%v", err)
	}

	result := h.settings.BuildDownloaderInfo(totals, today)
	configs := h.settings.DownloaderConfigs()
	configByID := map[string]map[string]any{}
	for _, cfg := range configs {
		id := handlerToString(cfg["id"], "")
		if id == "" {
			continue
		}
		configByID[id] = cfg
	}

	for _, item := range result {
		details := ensureDetailsMap(item["details"])
		item["details"] = details

		enabled := handlerToBool(item["enabled"], false)
		if !enabled {
			item["status"] = "未配置"
			continue
		}

		downloaderID := handlerToString(item["id"], "")
		downloaderName := handlerToString(item["name"], "下载器")
		cfg, ok := configByID[downloaderID]
		if !ok {
			item["status"] = "连接失败"
			details["错误信息"] = "未找到下载器配置"
			logx.Warnf(downloaderInfoModule, "下载器配置缺失 id=%s name=%s", downloaderID, downloaderName)
			continue
		}

		version, detectErr := resolveDownloaderVersion(cfg)
		if detectErr != nil {
			item["status"] = "连接失败"
			details["错误信息"] = detectErr.Error()
			logx.Warnf(downloaderInfoModule, "连接探测失败 id=%s name=%s err=%v", downloaderID, downloaderName, detectErr)
			continue
		}

		item["status"] = "已连接"
		details["版本"] = version
		delete(details, "错误信息")
	}

	c.JSON(http.StatusOK, result)
}

func ensureDetailsMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func resolveDownloaderVersion(cfg map[string]any) (string, error) {
	typ := strings.ToLower(strings.TrimSpace(handlerToString(cfg["type"], "")))
	if typ == "" {
		return "", errors.New("缺少下载器类型配置")
	}
	downloader := downloaderclient.Downloader{
		ID:       handlerToString(cfg["id"], ""),
		Name:     handlerToString(cfg["name"], ""),
		Type:     typ,
		Host:     strings.TrimSpace(handlerToString(cfg["host"], "")),
		Username: handlerToString(cfg["username"], ""),
		Password: handlerToString(cfg["password"], ""),
		APIKey:   handlerToString(cfg["api_key"], ""),
		Enabled:  true,
	}
	if downloader.Host == "" {
		return "", errors.New("缺少 host 配置")
	}
	return downloader.FetchVersion()
}

func handlerToBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		lowered := strings.ToLower(strings.TrimSpace(typed))
		if lowered == "true" || lowered == "1" || lowered == "yes" {
			return true
		}
		if lowered == "false" || lowered == "0" || lowered == "no" {
			return false
		}
		return fallback
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return fallback
	}
}

func handlerToInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}
