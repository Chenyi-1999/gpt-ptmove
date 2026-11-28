# StarPT v1.2.0

- 打开「设置 → 布局 → 首页」，可调整电脑端列数和卡片顺序，并勾选要显示的卡片，包括任务卡。任务卡占一整行。
- 打开「设置 → 布局 → 做种检索」，可单独显示或隐藏每个模块。已有列数与顺序可继续使用，手机端固定单列。
- 隐藏首页任务卡后，原有任务仍在后台正常运行，并可通过任务记录查看。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。使用 Compose 升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.2.0-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.2.0-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.2.0/starpt-v1.2.0-linux-amd64.tar.gz)
