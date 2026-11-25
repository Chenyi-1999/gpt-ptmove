# STARPT v1.0.12

- 北洋园插件增加「使用站点上传限速」开关。开启后，试运行和正式循环新加入的 qB 任务读取「设置 → 站点管理 → 北洋园」的上传限速。
- 开关默认关闭，可在 RSS 循环运行时直接切换。页面展示当前站点设置，既有 qB 任务继续使用原有配置。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.12) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.12)

```sh
sha256sum -c starpt-v1.0.12-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.12-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 为 `280375fb2fc982b96e3975a17c91582004d16f7365d6ff32909d2d15ca75c190`。
