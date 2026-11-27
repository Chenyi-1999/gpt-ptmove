# STARPT v1.1.6

- PTGen 来源、路径映射和新添加但未保存的下载器可以点击移除。表单变更在保存后生效，自定义标签继续使用行内的 ×。
- 删除站点、已保存下载器、背景文件、发布记录、批量种子资料和撤销机器密钥仍要求长按 2 秒。
- 路径映射保存失败后，取消编辑会保留上次保存的规则。

## Docker 发行

GitHub 提供完整的 Linux AMD64 镜像、Compose 和 SHA-256 校验文件。Gitee 提供 Compose、校验文件和完整镜像入口。

```sh
sha256sum -c starpt-v1.1.6-linux-amd64.tar.gz.sha256
docker load -i starpt-v1.1.6-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```
