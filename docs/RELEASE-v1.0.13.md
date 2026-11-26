# STARPT v1.0.13

- 下载器设置保留「直连下载器」和「远程截图与 MediaInfo」两种方式，区分下载器地址、媒体代理端口和飞牛客户端的 HTTPS 领单端口。
- 两种方式都通过配置的下载器 API 读取版本、种子列表和流量。启用远程媒体时，在此基础上使用独立媒体通道处理截图和 MediaInfo。
- 飞牛沿用原有 Hub 地址、媒体端口、凭据和 HTTPS 领单配置。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.13) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.13)

```sh
sha256sum -c starpt-v1.0.13-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.13-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
