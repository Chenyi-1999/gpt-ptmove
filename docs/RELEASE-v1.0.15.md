# STARPT v1.0.15

- 北洋园订阅统一使用「北洋园」标签，新任务确认加入 qB 后回收临时标签。
- 存量任务逐批核对下载器、种子哈希与加入时间，先保存归属，再移除已确认任务的旧标签。H&R 页面同时读取两种标签，自动退种继续要求台账匹配、站点确认达标或免考，并复核上传状态。
- 回退旧版前先暂停北洋园 RSS。已迁移的任务需要按台账恢复旧标签后，旧版才能继续管理。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.15) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.15)

```sh
sha256sum -c starpt-v1.0.15-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.15-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
