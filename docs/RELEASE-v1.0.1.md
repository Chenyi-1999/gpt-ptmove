# STARPT v1.0.1

- 工作台新增实时速度卡片，显示近 60 秒的上传和下载速率，每 2 秒刷新一次。
- 可从卡片右上角切换下载器。桌面和手机都使用站内深色下拉框，菜单在页面内展开。
- 近期速率接口读取尚未写入数据库的采样，刷新可以及时显示新数据。

## 下载与部署

[GitHub 私有发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.1)提供完整 Linux AMD64 镜像、Compose 和 SHA-256 校验文件。Gitee 发行附带 Compose 和校验文件，完整镜像通过同版本 GitHub 私有发行提供。[Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.1) · [Gitee 附件限制](https://help.gitee.com/repository/release/create)

将 `starpt-v1.0.1-linux-amd64.tar.gz`、校验文件与 `compose.starpt.yaml` 放在同一目录。

```sh
sha256sum -c starpt-v1.0.1-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.1-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
docker compose -f compose.starpt.yaml ps
```

Compose 使用 `ptmove-nexus:1.0.1`，默认管理端口 `18120`，数据目录 `./starpt-data`。升级时沿用当前实例的数据目录和运行配置。[备份与回滚](../README.md#数据备份升级与回滚)

完整镜像大小为 581,373,136 字节，SHA-256 为 `8206e95d634488b75940a9cdcc2e806146ec7da743c9e5227f92713f41ca75d0`。152 已以空数据目录运行 Compose 验证，线上健康和版本接口返回 `1.0.1`。
