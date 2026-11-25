# STARPT v1.0.11

- 北洋园插件的在管任务数可填 0（不限数量）或任意正整数。原有总容量、单种体积与 H&R 退种规则继续生效。
- 超过 50 条任务时，插件分批查询 qB 和 H&R，保留全部标签内任务的考核显示。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.11) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.11)

```sh
sha256sum -c starpt-v1.0.11-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.11-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 `7204af4528d9507af36bf2ac4bab678fd274a9aafd278dd6c083778c0167ef02`。
