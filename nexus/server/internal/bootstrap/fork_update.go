package bootstrap

import "github.com/gin-gonic/gin"

const forkVersion = "0.3.1"

// registerForkUpdateRoutes 提供本改版的离线版本信息；升级通过已校验的 Docker 发布包完成。
// 不请求上游升级服务，也不下载或安装原版二进制，避免覆盖本改版模块和数据结构。
func registerForkUpdateRoutes(engine *gin.Engine) {
	engine.GET("/update/local-version", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "local_version": forkVersion, "local_version_source": "ptmove-nexus"})
	})
	engine.GET("/update/check", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "has_update": false, "local_version": forkVersion, "remote_version": forkVersion, "reason_code": "already_latest", "reason_message": "STARPT 通过 Gitee 发布的 Docker 镜像更新", "update_control": gin.H{"disable_update": true, "force_update": false}})
	})
	engine.GET("/update/changelog", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "history": []gin.H{{"version": forkVersion, "date": "2026-11-15", "changes": []string{"完善 API 管理、密钥状态及只读连接验证", "新增 AI 接入包、权限预设与稳定 OpenAPI 操作标识"}}, {"version": "0.3.0", "date": "2026-10-27", "changes": []string{"基于 PTNexus 原生 Go + Vue 项目，保留原手动转种界面", "新增可启停的自动下载与转种模块、站点 JSON 声明导入和独立 AI API"}, "note": "发布地址：https://gitee.com/star1999/gpt-ptmove/releases"}}})
	})
	for _, endpoint := range []string{"/update/pull", "/update/install"} {
		engine.POST(endpoint, func(c *gin.Context) {
			c.JSON(409, gin.H{"success": false, "message": "请使用本改版 Gitee 发布包更新 Docker，不支持原版在线覆盖安装"})
		})
	}
}
