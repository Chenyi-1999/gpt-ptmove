# STARPT v1.0.10

- 北洋园插件统一保存 RSS、H&R Token、qB 下载器、标签和分类。试运行最多添加一个暂停任务。正式循环默认每分钟读取 RSS。
- 可启用达标后低速清理。两次 H&R 确认至少相隔 15 分钟，并且上传连续低于所设阈值才移除 qB 任务。功能默认关闭，下载文件保留。
- H&R 列表继续显示站点的实际剩余做种时间。RSS 与自动移除从同一个插件入口配置。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.10) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.10)

```sh
sha256sum -c starpt-v1.0.10-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.10-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 `45eb975030a3dc19e5518e363f7bc03b36dd5491f73f9a6db3293240c1120693`。
