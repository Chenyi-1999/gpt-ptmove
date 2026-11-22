# STARPT v1.0.4

- 自动化先筛选织梦官种，排除分集、禁转和已下载的资源。NovaHD 查重只使用实际搜索结果，结合版本和载荷核对。确认目标缺失后才交给 qB。
- qB 确认每条文件完整后，复用已有的截图和 MediaInfo，缺失时走既有 HTTPS 客户端补全。通过校验的自动截图可继续发布。无法完成补全的任务显示「待补资料」。
- 新发布、目标已存在和结果待核对各有状态。已有资源不计转种成功。提交结果不明时保留核对入口。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和 SHA-256 校验文件。Gitee 发行提供 Compose 和校验文件，完整镜像从同版本 GitHub 私有发行下载。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.4) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.4)

将镜像、校验文件和 `compose.starpt.yaml` 放在同一目录。

```sh
sha256sum -c starpt-v1.0.4-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.4-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
docker compose -f compose.starpt.yaml ps
```

Compose 使用 `ptmove-nexus:1.0.4`。默认访问端口 `18120`，数据目录 `./starpt-data`。升级时沿用现有数据目录和配置。 [备份与回滚](../README.md#数据备份升级与回滚)

152 已验证独立 Compose、线上健康和版本接口。Go 全量测试覆盖目标查重、补资料、队列交接、取消以及重复回执。Vue 类型检查、定向 ESLint 和生产构建均通过。部署保持自动执行、定时计划和自动发布关闭。

实站只读预览按织梦 → NovaHD、6–7 GiB、至少 3 人做种运行。100 条中 20 条通过，另有 12 条确认目标已存在、18 条查重证据不足。指定的两条历史下载记录均被跳过。预览用时约 8 分 39 秒，设置、任务、配额和发布记录保持一致。

完整 Linux AMD64 镜像归档为 673,404,753 字节，SHA-256 为 `e59f5fb5bee852ce465ded12d0e9c418b5ad6ffea2804f97ad6b1ee3249f11a4`。
