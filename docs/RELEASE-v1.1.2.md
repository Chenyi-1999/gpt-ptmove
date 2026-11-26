# STARPT v1.1.2

- 「设置 → 转种」独立显示转种、发种与下载器标签设置。旧设置地址也保留在设置页。
- 「插件 → 自动转种」管理自动规则、候选预览和任务。任务面板打开自动化任务时，直接定位到该插件。

## Docker 发行

GitHub 提供完整 Linux AMD64 镜像、Compose、校验文件和示例插件。Gitee 提供 Compose、校验文件、示例和完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.1.2) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.1.2)

```sh
sha256sum -c starpt-v1.1.2-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.2-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
