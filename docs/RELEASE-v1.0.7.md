# STARPT v1.0.7

- 首页「任务」卡片与全局任务抽屉共享状态。候选预览显示正在处理的条目和最终统计，短任务的结果会保留。并行操作按各自的任务更新。
- 维护和转种草稿新增「一键补全截图与 MediaInfo」。保留已有资料，缺截图时自动挑选 5 张，用户可重新选择。任何资料回填都留在草稿中。
- 本机截图按实际截取和上传张数更新。现有飞牛 HTTPS 客户端在处理时显示阶段，收到整批响应后显示实际数量。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.7) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.7)

```sh
sha256sum -c starpt-v1.0.7-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.7-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

完整镜像 SHA-256 `e9b2be9aad5680d6dfe56622d1af12954e302bb2e29b33df2f3d8bdc166b56b4`。
