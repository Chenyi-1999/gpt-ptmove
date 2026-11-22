# STARPT v1.0.5

- 自动化先处理符合规则的较小种子。设置页移除月度体积预算，单种体积区间与磁盘预留检查继续生效。
- 织梦下载前读取动态「辅种」栏。下载完整后按批用 IYUU 核对目标，默认每批 20 个。月度名额按确认发布计，重复项支持保留、移动，或移除 qB 任务并保留文件。
- 工作台用一张卡片显示并行任务、当前阶段和 qB 下载百分比。

## 下载与部署

完整 Linux AMD64 镜像、Compose 和校验文件从 GitHub 私有发行下载。Gitee 发行提供 Compose、校验和镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.5) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.5)

```sh
sha256sum -c starpt-v1.0.5-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.5-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

默认使用端口 `18120` 和数据目录 `./starpt-data`。升级时沿用现有数据目录与配置。 [备份与回滚](../README.md#数据备份升级与回滚)

152 的 Go 全量、前端构建和隔离 API 冒烟测试通过。实站只读取织梦列表，确认 6–7 GiB 区间第一页 100 条按体积升序。线上保留 1.0.4 回滚镜像，验证期间自动执行、定时计划和自动发布均关闭。

完整镜像为 656,431,682 字节，SHA-256 为 `1143e9e774ee2db0fa810f4ff4a53bfc24e3aef38f2323486246ebb106164f57`。
