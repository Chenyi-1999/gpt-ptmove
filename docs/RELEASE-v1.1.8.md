# StarPT v1.1.8

- 下载器统计 2.1 支持按分类、标签、Tracker 和种子状态组合筛选。列表收起时显示数量，展开可分页查看种子名单。
- 插件页显示安装路径和刷新方法。站点管理的筛选切换和导入操作位于列表上方。
- 项目名称及源码、发行和反馈入口统一为 StarPT。
- 本地文件查询支持远程只读核验。北洋园刷流可通过 `tjupt-rss.zip` 安装。

## Docker 部署

下载 Linux amd64 完整镜像、校验文件和 Compose 到同一目录。已有实例升级时，把 `PTMOVE_DATA_DIR` 指向原有数据目录。

```sh
sha256sum -c starpt-v1.1.8-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.8-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

## 安装插件

把 `downloader-summary.zip` 复制到 StarPT 数据目录下的 `plugins/`。使用发行 Compose 默认设置时，宿主机路径为 `./starpt-data/plugins/`。然后打开插件页，点击「刷新插件」。北洋园插件的 ZIP 使用同一目录。

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.1.8/starpt-v1.1.8-linux-amd64.tar.gz)
