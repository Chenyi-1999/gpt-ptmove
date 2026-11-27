package handler

import (
	"context"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/plugins"
)

func (h *pluginHandler) tjuptReady() bool {
	info, err := h.packages.Get(plugins.TjuptFeedPluginID)
	return err == nil && info.Enabled && slices.Contains(info.Capabilities, plugins.TjuptFeedCapability)
}

func (h *pluginHandler) pauseUnavailableTjupt(ctx context.Context) error {
	if h.tjuptReady() {
		return nil
	}
	return h.tjupt.SetEnabled(ctx, false)
}

// setTjuptEnabled 同步包的可用状态。重新启用插件后仍需手动启动 RSS。
func (h *pluginHandler) setTjuptEnabled(c *gin.Context, enabled bool) {
	if !lockTjuptTransition(c, &h.tjuptTransition) {
		return
	}
	defer h.tjuptTransition.Unlock()
	info, err := h.packages.Get(plugins.TjuptFeedPluginID)
	if err != nil {
		pluginError(c, err)
		return
	}
	if !enabled || !info.Enabled {
		if err := h.tjupt.SetEnabled(c.Request.Context(), false); err != nil {
			tjuptFeedError(c, err, http.StatusServiceUnavailable, "北洋园循环暂时无法暂停")
			return
		}
	}
	info, err = h.packages.SetEnabled(plugins.TjuptFeedPluginID, enabled)
	if err != nil {
		pluginError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plugin": info})
}

// invokeTjupt 只执行已由外装包确认且与管理员本次请求相符的宿主动作。
func (h *pluginHandler) invokeTjupt(c *gin.Context, action string) {
	if !lockTjuptTransition(c, &h.tjuptTransition) {
		return
	}
	defer h.tjuptTransition.Unlock()
	result, err := h.packages.Invoke(c.Request.Context(), plugins.TjuptFeedPluginID, action, nil)
	if err != nil {
		pluginError(c, err)
		return
	}
	runTjuptFeedAction(c, h.tjupt, result.HostAction)
}
