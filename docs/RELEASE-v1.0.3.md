# STARPT v1.0.3

- 修复织梦做种人数读取。候选筛选以织梦种子列表中对应的做种人数为准。
- 织梦按体积范围和最少做种人数查询，从该范围的第一页开始按体积降序翻页，再用种子元数据复核精确体积。
- 保留官方种子限制，以及禁转、分集、账号已下载和 NovaHD 已有资源的排除规则。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和 SHA-256 校验文件。Gitee 发行提供 Compose 和校验文件，完整镜像从同版本 GitHub 私有发行下载。[GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.3) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.3) · [Gitee 附件限制](https://help.gitee.com/repository/release/create)

将镜像、校验文件和 `compose.starpt.yaml` 放在同一目录。

```sh
sha256sum -c starpt-v1.0.3-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.3-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
docker compose -f compose.starpt.yaml ps
```

Compose 使用 `ptmove-nexus:1.0.3`。默认访问端口 `18120`，数据目录 `./starpt-data`。升级时沿用已有数据目录和运行配置。[备份与回滚](../README.md#数据备份升级与回滚)

152 已验证独立 Compose、线上健康和版本接口。实站预览按 6–7 GiB、至少 3 人做种运行。返回 100 条，完整体积核验的 50 条均满足做种人数要求。NovaHD 查重判定 37 条已存在，13 条待确认，本轮没有通过项目。指定的两条历史下载记录均被跳过。

完整 Docker 镜像归档为 653,264,101 字节，SHA-256 为 `baa62d9c0e47f1537e441963153163c2c6b827901ab362d145419d6d74f9bd37`。
