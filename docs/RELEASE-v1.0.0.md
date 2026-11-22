# STARPT v1.0.0

- 转种准备自动识别简介引用中的 MediaInfo。优先使用已有完整报告，无法从简介确定报告时交由已配置的下载器代理补全。
- 全站采用深海军蓝和紫蓝配色。设置分类在窄屏换行，长表单与路径映射弹窗支持滚动。
- 工作台提供流量趋势与逐小时明细，站点页提供做种体积与官组排行。旧 `/info` 地址跳转到工作台。

## 下载与使用 Compose

GitHub 私有仓库的发行页提供完整 Linux AMD64 镜像、SHA-256 校验文件和 Compose。 [GitHub 发行页](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.0)

Gitee 同版本发行页提供 Compose 和完整镜像的下载入口。 [Gitee 发行页](https://gitee.com/star1999/starpt/releases/tag/v1.0.0)

将下载的 `starpt-v1.0.0-linux-amd64.tar.gz` 和 `compose.starpt.yaml` 放在同一目录。按发行页提供的 SHA-256 校验文件核对镜像后，用 Docker Compose v2 启动。

```sh
docker load -i starpt-v1.0.0-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
docker compose -f compose.starpt.yaml ps
```

Compose 使用已载入的 `ptmove-nexus:1.0.0`。默认管理端口为 `18120`，数据位于 Compose 文件旁的 `starpt-data`。首次安装可用 `docker compose -f compose.starpt.yaml logs starpt` 查看初始化登录提示。设置 `PTMOVE_BIND`、`PTMOVE_PORT`、`PTMOVE_DATA_DIR` 可以调整监听地址、端口和宿主机数据路径，容器运行配置可放在可选的 `.env.ptmove` 中。

升级已有实例时沿用原有数据目录与运行配置，并在切换前按项目指南备份。 [升级与回滚指南](../README.md#数据备份升级与回滚)

完整镜像为 543,336,327 字节，SHA-256 为 `8cd7616e6fce00b05858111a145fb4c4ca1409e12497f3bdb8223d10a1e148ac`。发行镜像已在 152 以空数据目录运行 Compose 验证，现网版本接口返回 `1.0.0`。
