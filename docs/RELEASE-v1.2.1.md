# StarPT v1.2.1

- 「设置 → 下载器」默认以折叠列表显示已保存的下载器。点击「编辑」可展开连接配置和路径映射，新增下载器自动展开。
- 保存成功后列表自动收起。手动收起会保留当前输入，可展开继续编辑。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。使用 Compose 升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.2.1-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.2.1-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

[完整 Docker 镜像下载](https://github.com/Chenyi-1999/StarPT/releases/download/v1.2.1/starpt-v1.2.1-linux-amd64.tar.gz)
