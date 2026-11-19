# STARPT 插件

将 UTF-8 JSON 文件放进 STARPT 数据目录中的 `plugins/`。Compose 默认对应宿主机的 `data/plugins/`，容器内为 `/app/data/plugins/`。打开「插件」，点击「刷新插件」。移除文件后再刷新即可移除对应入口。

例如保存为 `ops.json`：

```json
{
  "schema_version": 1,
  "id": "ops",
  "name": "转种工作台",
  "version": "1.0",
  "description": "集中查看待整理的资料和发布记录。",
  "blocks": [
    {"kind": "metric", "title": "转种资料数", "metric": "transfer_drafts"},
    {"kind": "link", "title": "打开任务记录", "target": "/publish-logs"},
    {"kind": "text", "title": "检查源站要求", "text": "发种前核对资料和来源：禁止转载的种子不能转发。"}
  ]
}
```

- `id` 与文件名相同，使用小写字母、数字和连字符。
- `text` 展示普通文本。`link` 打开应用内已有的功能，例如 `/seed-maintenance`、`/publish-logs`、`/settings/automation`、`/settings/site-profiles`、`/settings/api`。
- `metric` 支持 `visible_torrents`、`transfer_drafts`、`configured_sites`。
- 清单支持最多 16 个内容块，每个文件最多 64 KiB。错误文件会在插件页面标明。目录读取失败时，已加载的插件继续可用。

插件面板基于声明文件运行。需要新增数据读写或后台动作时，通过 STARPT 的服务代码增加可供插件调用的功能，发布仍受源站规则检查。
