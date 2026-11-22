package bootstrap

import "github.com/gin-gonic/gin"

const forkVersion = "1.0.0"

// registerForkUpdateRoutes 提供本改版的离线版本信息；升级通过已校验的 Docker 发布包完成。
// 不请求上游升级服务，也不下载或安装原版二进制，避免覆盖本改版模块和数据结构。
func registerForkUpdateRoutes(engine *gin.Engine) {
	engine.GET("/update/local-version", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "local_version": forkVersion, "local_version_source": "ptmove-nexus"})
	})
	engine.GET("/update/check", func(c *gin.Context) {
		c.JSON(200, gin.H{"success": true, "has_update": false, "local_version": forkVersion, "remote_version": forkVersion, "reason_code": "already_latest", "reason_message": "请从 STARPT 发行页下载 Docker 镜像更新", "update_control": gin.H{"disable_update": true, "force_update": false}})
	})
	engine.GET("/update/changelog", func(c *gin.Context) {
		history := []gin.H{
			{
				"version": forkVersion,
				"date":    "2026-11-22",
				"changes": []string{
					"转种准备自动识别简介中的 MediaInfo，并在缺失时交由下载器补全",
					"统一全站深色主题，修复设置页面和弹窗的滚动与窄屏布局",
					"工作台汇总流量趋势与小时明细，站点页新增做种与官组统计图表",
				},
			},
			{
				"version": "0.3.1",
				"date":    "2026-11-15",
				"changes": []string{"完善 API 管理、密钥状态及只读连接验证", "新增 AI 接入包、权限预设与稳定 OpenAPI 操作标识"},
			},
			{
				"version": "0.3.0",
				"date":    "2026-10-27",
				"changes": []string{"提供 Go + Vue 的手动转种界面", "新增可启停的自动下载与转种模块、站点 JSON 声明导入和独立 AI API"},
				"note":    "发布地址：https://gitee.com/star1999/gpt-ptmove/releases",
			},
		}
		c.JSON(200, gin.H{"success": true, "history": history})
	})
	for _, endpoint := range []string{"/update/pull", "/update/install"} {
		engine.POST(endpoint, func(c *gin.Context) {
			c.JSON(409, gin.H{"success": false, "message": "请使用 STARPT 发行包更新 Docker"})
		})
	}
}
