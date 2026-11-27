# STARPT v1.1.7

- 北洋园刷流通过发行附件 `tjupt-rss.zip` 安装，使用宿主已有的 RSS、qB、H&R 和任务台账。退种继续核对任务归属、站点确认与持续低速，保留本地文件。
- 从内置版升级后，把附件放进数据目录 `plugins/`，在插件页点击「刷新插件」，再打开「北洋园刷流」点击「正式启动」。原配置和台账可直接沿用。
- 停用或移除包时，主程序暂停 RSS 循环。再次启用或重装后，通过「正式启动」恢复。

- 本地文件查询支持通过远程客户端核验缺失和孤立文件，无法完成的目录会显示未核验。

## Docker 部署

合并镜像的标签为 `ptmove-nexus:1.1.7-remote-scan.1`。导入该镜像和对应校验文件后可使用以下 Compose。现有安装应沿用原数据挂载。

```sh
sha256sum -c starpt-v1.1.7-remote-scan.1-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.7-remote-scan.1-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
