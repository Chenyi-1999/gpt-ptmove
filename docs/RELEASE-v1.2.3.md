# StarPT v1.2.3

- 下载器折叠卡片根据连接方式显示「本地」或「远程」。开启「远程截图与 MediaInfo」显示「远程」，使用直连显示「本地」。
- 移除手动位置设置。现有连接方式、凭据及配置状态判断继续使用已有设置。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。使用 Compose 升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.2.3-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.2.3-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.2.3/starpt-v1.2.3-linux-amd64.tar.gz)
