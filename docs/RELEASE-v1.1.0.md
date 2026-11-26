# STARPT v1.1.0

- ZIP/WASM 插件只在点击「刷新插件」时扫描目录，支持新增、更新、移除和重启恢复。
- 插件页支持配置表单、运行、表格结果及可选定时计划。下载器统计示例可直接安装，源码含打包工具。
- 自动转种作为内置插件使用独立总开关，既有规则、预览、任务记录及 AI API 沿用。

## 下载与部署

GitHub 公开发行提供完整 Linux AMD64 Docker 镜像、Compose、校验文件与下载器统计示例。Gitee 私有发行提供 Compose、校验文件、示例和完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.1.0) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.1.0)

```sh
sha256sum -c starpt-v1.1.0-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.0-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

将 `downloader-summary.zip` 放进现有数据目录的 `plugins/`，点击「刷新插件」。开发与端口说明见 [STARPT 插件](STARPT_PLUGINS.md)。
