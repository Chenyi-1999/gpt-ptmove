# STARPT v1.0.6

自动化的候选预览在种子标题下方提供「详情」。点击后在新标签页打开源站详情，保留预览和已选项目。已排除的候选也可以查看源站详情。

## 下载与部署

完整 Linux AMD64 镜像、Compose 和校验文件从 GitHub 私有发行下载。Gitee 发行提供 Compose、校验和镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.6) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.6)

```sh
sha256sum -c starpt-v1.0.6-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.6-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

默认访问端口 `18120`。升级时沿用现有数据目录与配置。 [备份与回滚](../README.md#数据备份升级与回滚)

152 构建、隔离 API 验证及线上健康检查通过，保留 1.0.5 回滚镜像。完整镜像 SHA-256 为 `93b09b9635312eb45c169df8a18b70713a6588a2822cb345540df33dbb869e8d`。
