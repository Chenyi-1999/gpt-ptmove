# StarPT v1.1.9

- 在「设置 → 布局」配置做种检索的电脑端列数和模块优先级。此前保存的布局可直接沿用。
- 手机端的做种检索继续使用单列。

- 移除通用设置中的「功能扩展」占位卡。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。使用 Compose 升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.1.9-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.9-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.1.9/starpt-v1.1.9-linux-amd64.tar.gz)
