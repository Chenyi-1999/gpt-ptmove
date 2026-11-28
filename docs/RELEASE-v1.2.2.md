# StarPT v1.2.2

- 「设置 → 下载器」的折叠列表显示位置、连接方式和配置状态。已有明确位置标记和本机回环地址可自动识别，其余地址可在「编辑 → 下载器位置」手动标注。
- 「已配置」表示必要配置字段已齐。qBittorrent 账号凭据和媒体代理令牌仍以脱敏状态显示。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。使用 Compose 升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.2.2-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.2.2-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.2.2/starpt-v1.2.2-linux-amd64.tar.gz)
