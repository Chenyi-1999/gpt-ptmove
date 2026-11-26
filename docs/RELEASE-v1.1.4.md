# STARPT v1.1.4

- 「设置 → 转种」增加可排序的 PTGen 来源列表。点加号新增一行，先选接口类型，再填地址和可选令牌。支持自动识别、财神、PT-Gen Universal、OurBits 和通用接口。
- 自选来源按列表顺序查询，取得当次需要的海报或简介后停止。所有自选来源未取得结果时，继续用内置来源。财神没有固定位置，旧令牌可在手动添加财神后选择使用。

## Docker 发行

GitHub 提供完整 Linux AMD64 镜像、Compose、校验文件和示例插件。Gitee 提供 Compose、校验文件、示例和完整镜像入口。 [GitHub 发行](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.1.4) · [Gitee 发行](https://gitee.com/star1999/starpt/releases/tag/v1.1.4)

```sh
sha256sum -c starpt-v1.1.4-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.4-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
