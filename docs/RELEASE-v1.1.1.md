# STARPT v1.1.1

- 在插件页点击「刷新插件」时，如果已装载插件的 WASM 缓存缺失或摘要不符，使用原 ZIP 重建，经校验后恢复插件动作。
- 运行期间插件状态文件缺失或损坏时，用当前内存的已装载状态重写，保留启停、配置及定时计划。坏 ZIP 继续保留上一次通过校验的版本。

## Docker 发行

GitHub 提供完整 Linux AMD64 镜像、Compose、校验文件与示例插件。Gitee 提供 Compose、校验文件、示例与完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.1.1) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.1.1)

```sh
sha256sum -c starpt-v1.1.1-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.1-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
