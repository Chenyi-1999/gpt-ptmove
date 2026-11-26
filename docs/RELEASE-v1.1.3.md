# STARPT v1.1.3

- 「设置 → 转种」的匿名发布选项改成两档滑块。左侧禁用匿名，右侧启用匿名，选中状态有高亮。支持拖动、点选和键盘方向键。
- 点击「保存」后，匿名发布设置才生效。

## Docker 发行

GitHub 提供完整 Linux AMD64 镜像、Compose、校验文件和示例插件。Gitee 提供 Compose、校验文件、示例和完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.1.3) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.1.3)

```sh
sha256sum -c starpt-v1.1.3-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.3-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
