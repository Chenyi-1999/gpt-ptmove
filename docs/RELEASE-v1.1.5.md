# STARPT v1.1.5

- 「设置 → 站点配置档案」新增「编写说明」。可以查看准备资料、生成和导入的步骤，一键复制 AI 提示词，并下载示例 JSON。
- 本版也包含此前上线的紧凑导航、首页任务优先和站点筛选位置调整。站点配置字段及导入规则沿用已有版本。

## Docker 发行

GitHub 提供完整 Linux AMD64 镜像、Compose 和 SHA-256 校验文件。Gitee 提供 Compose、校验文件和完整镜像入口。

```sh
sha256sum -c starpt-v1.1.5-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.5-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
