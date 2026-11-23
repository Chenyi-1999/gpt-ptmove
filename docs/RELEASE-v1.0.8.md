# STARPT v1.0.8

- 在「种子维护」列表勾选多条记录，点击「补全所选截图和媒体信息」，由后台逐条补齐缺项。已有有效截图和媒体报告会保留。
- 缺 MediaInfo 时先从简介识别，再读对应的完整视频。缺截图时回填五张不同的 Pixhost 图片，维护详情仍可重选截图。
- 首页「任务」显示当前条目、处理阶段以及已补全、已有资料、部分补全和失败统计。本批次仅更新本地维护资料。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.8) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.8)

```sh
sha256sum -c starpt-v1.0.8-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.8-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 `d8974cc348e7b539cd60ca09b1dee1a3ed9d137ce79973cdbcaff2246593724f`。
