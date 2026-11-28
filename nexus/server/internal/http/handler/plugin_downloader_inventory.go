package handler

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server/internal/service/plugins"
	"github.com/pt-nexus/server/internal/service/torrentdata"
)

// downloaderInventory 供已启用的插件读取下载器、筛选汇总及分页详情。
func (h *pluginHandler) downloaderInventory(c *gin.Context) {
	info, err := h.packages.Get(plugins.DownloaderInventoryPluginID)
	switch {
	case err != nil:
		pluginError(c, err)
		return
	case !slices.Contains(info.Capabilities, plugins.DownloaderInventoryCapability):
		pluginError(c, plugins.ErrPackageAction)
		return
	case !info.Enabled:
		pluginError(c, plugins.ErrPackageDisabled)
		return
	}

	id := c.Query("downloader_id")
	if id == "" {
		rows := h.inventoryList(false)
		result := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			if key, ok := row["id"].(string); ok && key != "" {
				result = append(result, gin.H{"id": key, "name": row["name"], "enabled": row["enabled"]})
			}
		}
		c.JSON(http.StatusOK, gin.H{"downloaders": result})
		return
	}

	ctx := c.Request.Context()
	dimension := c.DefaultQuery("dimension", "category")
	filters := torrentdata.InventoryFilters{
		Category: c.Query("category"),
		Tag:      c.Query("tag"),
		Tracker:  c.Query("tracker"),
		Status:   c.Query("status"),
	}
	if c.Query("details") == "1" {
		page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "25"))
		if pageErr != nil || sizeErr != nil {
			respondInventoryError(c, torrentdata.ErrInventoryQuery)
			return
		}
		result, err := h.inventory.Details(ctx, id, dimension, c.Query("group"), filters, page, pageSize)
		if err != nil {
			respondInventoryError(c, err)
			return
		}
		c.JSON(http.StatusOK, result)
		return
	}

	result, err := h.inventory.Summary(ctx, id, dimension, filters, c.Query("refresh") == "1")
	if err != nil {
		respondInventoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func respondInventoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, torrentdata.ErrInventoryQuery):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, torrentdata.ErrInventoryDownloader):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, torrentdata.ErrInventoryExpired):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	case errors.Is(err, torrentdata.ErrInventoryLimit):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"message": err.Error()})
	case errors.Is(err, torrentdata.ErrInventoryRead):
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "暂时无法读取下载器统计，请稍后重试"})
	}
}
