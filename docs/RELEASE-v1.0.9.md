# STARPT v1.0.9

- 自动化设置会恢复最近一次成功的候选预览，并显示预览时间。刷新网页、重启或重建 Docker 后，保存在数据库中的结果仍可读取。
- 新一次预览成功时更新列表。预览失败时保留原结果。把选中的候选加入队列时仍会使用当前规则复查。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.9) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.9)

```sh
sha256sum -c starpt-v1.0.9-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.9-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 `6bd74930a232760202d55761ef20fb8d04431e82e53e04993eee41a5b0c9db85`。
