# StarPT v1.2.4

- 下载器折叠卡片显示单一的「本地」或「远程」标记。连接方式使用两个清晰选项，通过信息图标查看说明。
- 选择「远程」后，可从 GitHub 下载客户端源码并复制 AI 部署提示词。

## Docker 升级

下载完整 Linux amd64 Docker 镜像、校验文件和 Compose。分卷附件的合并和校验方法随发行附件提供。升级现有实例时，将 `PTMOVE_DATA_DIR` 指向原数据目录。

```sh
sha256sum -c starpt-v1.2.4-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.2.4-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
