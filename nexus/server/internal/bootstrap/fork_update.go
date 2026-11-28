package bootstrap

import "github.com/gin-gonic/gin"

const forkVersion = "1.2.3"

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
				"date":    "2026-11-28",
				"changes": []string{"下载器位置跟随连接方式，移除手动位置设置"},
			},
			{
				"version": "1.2.2",
				"date":    "2026-11-28",
				"changes": []string{"下载器列表显示本地或远程位置、连接方式和配置状态"},
			},
			{
				"version": "1.2.1",
				"date":    "2026-11-28",
				"changes": []string{"下载器设置默认折叠已保存的配置，点击编辑展开"},
			},
			{
				"version": "1.2.0",
				"date":    "2026-11-28",
				"changes": []string{"首页支持调整卡片显示、顺序和电脑端列数", "做种检索可勾选显示的模块"},
			},
			{
				"version": "1.1.9",
				"date":    "2026-11-28",
				"changes": []string{"设置增加独立的布局入口，可调整做种检索的列数和模块顺序", "移除通用设置中的功能扩展占位卡"},
			},
			{
				"version": "1.1.8",
				"date":    "2026-11-28",
				"changes": []string{"下载器统计 2.1 支持组合筛选和展开种子名单", "插件页直接显示安装路径和刷新方法", "项目名称统一为 StarPT"},
			},
			{
				"version": "1.1.7",
				"date":    "2026-11-27",
				"changes": []string{"北洋园刷流改为外装 WASM 插件，保留宿主设置、任务台账和退种规则", "本地文件查询支持通过 HTTPS 领单进行远程只读扫描"},
			},
			{
				"version": "1.1.6",
				"date":    "2026-11-27",
				"changes": []string{"路径映射和 PTGen 来源可点击移除草稿行，未保存的下载器也可点击移除", "已保存的下载器和其他重要删除继续长按 2 秒"},
			},
			{
				"version": "1.1.5",
				"date":    "2026-11-27",
				"changes": []string{"站点配置档案增加编写说明、AI 提示词复制和示例 JSON 下载"},
			},
			{
				"version": "1.1.4",
				"date":    "2026-11-26",
				"changes": []string{"转种设置增加可排序的 PTGen 来源，可按顺序查询海报和简介"},
			},
			{
				"version": "1.0.16",
				"date":    "2026-11-26",
				"changes": []string{"项目源码、问题反馈和浏览器扩展入口指向 GitHub"},
			},
			{
				"version": "1.0.15",
				"date":    "2026-11-26",
				"changes": []string{"北洋园订阅使用统一标签，已确认归属的历史任务分批更新标签"},
			},
			{
				"version": "1.0.14",
				"date":    "2026-11-26",
				"changes": []string{"下载器设置用通用名称说明远程媒体客户端和 HTTPS 领单端口"},
			},
			{
				"version": "1.0.13",
				"date":    "2026-11-26",
				"changes": []string{"下载器连接方式简化为直连下载器和远程截图与 MediaInfo，保留飞牛 HTTPS 领单设置"},
			},
			{
				"version": "1.0.12",
				"date":    "2026-11-26",
				"changes": []string{"北洋园插件增加站点上传限速开关，新加种可读取站点管理页的上传限速"},
			},
			{
				"version": "1.0.11",
				"date":    "2026-11-26",
				"changes": []string{"北洋园插件支持不限在管任务数，保留总容量和单种体积规则", "超过 50 个种子也可以查询 H&R"},
			},
			{
				"version": "1.0.10",
				"date":    "2026-11-26",
				"changes": []string{"北洋园插件集中配置 RSS、试运行和正式循环", "两次确认 H&R 达标且持续低速后，可自动移除 qB 任务并保留文件"},
			},
			{
				"version": "1.0.9",
				"date":    "2026-11-23",
				"changes": []string{"候选预览结果保存在数据库中，刷新页面或重建 Docker 后恢复最近一次成功预览"},
			},
			{
				"version": "1.0.8",
				"date":    "2026-11-23",
				"changes": []string{"在种子维护列表勾选多条记录，一键补全缺失截图和 MediaInfo", "后台逐条处理，首页任务显示进度与分项结果"},
			},
			{
				"version": "1.0.7",
				"date":    "2026-11-23",
				"changes": []string{"首页任务显示候选预览、截图与媒体报告的真实阶段及最近结果", "转种维护可一键补齐缺失截图和 MediaInfo，自动选图后仍可手动重选"},
			},
			{
				"version": "1.0.6",
				"date":    "2026-11-23",
				"changes": []string{"候选预览支持从新标签页打开源站种子详情"},
			},
			{
				"version": "1.0.5",
				"date":    "2026-11-23",
				"changes": []string{
					"移除月度体积预算，优先下载体积较小的合格种子",
					"织梦下载前查看辅种栏，完成下载后按批用 IYUU 核对目标站",
					"工作台集中展示进行中任务和实际下载进度",
				},
			},
			{
				"version": "1.0.4",
				"date":    "2026-11-22",
				"changes": []string{
					"NovaHD 查重只核对实际搜索结果和相符的发行或载荷",
					"下载完成后通过既有客户端补齐截图和 MediaInfo",
					"目标种子已存在时单独记录，不计入成功",
				},
			},
			{
				"version": "1.0.3",
				"date":    "2026-11-22",
				"changes": []string{
					"修复织梦列表做种人数读取，并按体积降序跨页筛选符合规则的候选",
					"保留来源已下载、禁转和分集种子的跳过规则",
				},
			},
			{
				"version": "1.0.2",
				"date":    "2026-11-22",
				"changes": []string{
					"自动化只选源站官种，排除禁转、分集、已下载和目标站已有的种子",
					"织梦候选先按体积范围与降序查询，再核对站点标签、完整大小和做种人数",
				},
			},
			{
				"version": "1.0.1",
				"date":    "2026-11-22",
				"changes": []string{
					"工作台显示近 60 秒的上传和下载速度，每 2 秒刷新",
					"实时速度卡片支持切换下载器，使用站内统一的深色下拉框",
				},
			},
			{
				"version": "1.0.0",
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
				"note":    "发布地址：https://github.com/Chenyi-1999/StarPT/releases",
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
