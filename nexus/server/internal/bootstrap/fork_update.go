package bootstrap

import "github.com/gin-gonic/gin"

// registerForkUpdateRoutes 提供本改版的离线版本信息；升级通过已校验的 Docker 发布包完成。
// 不请求上游升级服务，也不下载或安装原版二进制，避免覆盖本改版模块和数据结构。
func registerForkUpdateRoutes(engine *gin.Engine) {
	engine.GET("/update/local-version", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "local_version": "0.3.0", "local_version_source": "ptmove-nexus"})
	})
	engine.GET("/update/check", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "has_update": false, "local_version": "0.3.0", "remote_version": "0.3.0", "reason_code": "already_latest", "reason_message": "PTMOVE 改版通过 Gitee 发布的 Docker 镜像更新", "update_control": gin.H{"disable_update": true, "force_update": false}})
	})
	engine.GET("/update/changelog", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "history": []gin.H{{"version": "0.3.0", "date": "2026-10-27", "changes": []string{"基于 PTNexus 原生 Go + Vue 项目，保留原手动转种界面", "新增可启停的自动下载与转种模块、站点 JSON 声明导入和独立 AI API"}, "note": "发布地址：https://gitee.com/star1999/gpt-ptmove/releases"}}})
	})
	for _, endpoint := range []string{"/update/pull", "/update/install"} {
		engine.POST(endpoint, func(c *gin.Context) {
			c.JSON(409, gin.H{"success": false, "message": "请使用本改版 Gitee 发布包更新 Docker，不支持原版在线覆盖安装"})
		})
	}
}
