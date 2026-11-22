# STARPT v1.0.2

- 自动化只选源站官种。禁转、分集、织梦账号下载进度、下载器中的相同种子和 STARPT 旧下载任务都会排除。
- 织梦先按「官方」标签、体积范围和做种人数查询，由大到小排序，再用详情和种子元数据核对实际体积。目标站明确没有对应种子才通过查重。
- 预览会显示跳过原因，禁选未通过的项目。实际下载前和发种前重新核对资格。已由本任务下载的数据仍可继续本任务的后续流程。

## 下载与部署

GitHub 私有发行提供完整 Linux AMD64 Docker 镜像、Compose 和 SHA-256 校验文件。Gitee 发行提供 Compose 和校验文件，完整镜像从同版本 GitHub 私有发行下载。[GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.2) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.0.2) · [Gitee 附件限制](https://help.gitee.com/repository/release/create)

将镜像、校验文件和 `compose.starpt.yaml` 放在同一目录。

```sh
sha256sum -c starpt-v1.0.2-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.0.2-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
docker compose -f compose.starpt.yaml ps
```

Compose 使用 `ptmove-nexus:1.0.2`。默认访问端口 `18120`，数据目录 `./starpt-data`。升级时沿用已有数据目录和运行配置。[备份与回滚](../README.md#数据备份升级与回滚)

完整镜像 600,401,955 字节，SHA-256 为 `0d325567f2cd4fd725bc58093b99b6b6f9d4e2f637ef9f489c16e7fc3f735edc`。152 已验证空目录 Compose、健康与版本接口。自动化筛选已使用隔离预览和回归测试验证。
