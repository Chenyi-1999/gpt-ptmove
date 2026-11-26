# STARPT v1.0.16

- 菜单中的「项目源码」和「问题反馈」链接到 GitHub 仓库与 Issues。站点设置中的浏览器扩展链接也使用 GitHub。

## 下载与部署

GitHub 公开发行提供完整 Linux AMD64 Docker 镜像、Compose 和校验文件。Gitee 私有发行提供 Compose、校验文件及完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.16) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.16)

[项目源码](https://github.com/Chenyi-1999/gpt-ptmove) · [问题反馈](https://github.com/Chenyi-1999/gpt-ptmove/issues)

```sh
sha256sum -c starpt-v1.0.16-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.16-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
