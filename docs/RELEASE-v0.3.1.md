# PTMove Nexus v0.3.1

基于 PTNexus 的 API 管理和 NAS 远程补截图版本，已在 152 的 Docker 服务部署验证。[Gitee 正式发行版](https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.1) 于 2026-11-16 发布，包含十个 Docker 发布包分片及 NAS 独立源码安装包，共 11 个附件。标签保持指向已验证的功能提交 `ed0bcc6a39223993368d3489db7172bb1678bf72`。

## 变化

- 原设置页增加 API 管理：具名密钥、独立读取/写入/发布权限、有效期、撤销、连接测试、OpenAPI 和不含真实密钥的 AI 接入包。旧 `/settings/ai` 兼容跳转。
- 缺少或失效的视频截图可由 NAS 端代理生成并上传 Pixhost，返回完整图片地址及 BBCode；保留有效旧图并去重。字幕未知或截图不足时保留待确认，自动发布不能越过审核状态。
- NAS 代理提供 Linux amd64 和 arm64 Docker 镜像，独立 Token、目录白名单、媒体只读挂载、非 root 和资源限制。qB 与媒体代理使用各自凭据。
- 保留站点 JSON 导入、原生手动转种和可启停自动化。修复机器 API 的 401 误触发管理员退出；原登录和已有配置在升级后保留。

## 安装和恢复

现有部署：[152 管理页面](http://152.136.236.238:18120/)，[API 管理](http://152.136.236.238:18120/settings/api)。自动化、计划和自动发布默认关闭。

完整历史发布包包含源码和三个 Docker 归档；若使用分片附件，下载全部分片 ZIP，解压到同一目录后执行其中的 `python3 join_release_parts.py`。脚本会验证分片和完整包 SHA256；不要直接执行 `.zip.001` 文件。

| Docker 归档 | SHA256 |
| --- | --- |
| ptmove-nexus-0.3.1-linux-amd64.tar.gz | `7c7fa54b769f400cd468557b749ab4566805ca4672d769ea995fe9a893004f6a` |
| ptmove-media-agent-0.3.1-linux-amd64.tar.gz | `b23c667b6f11ef808b1c45b92f536a4a28d273a0112107c57d76bf431d76a5fb` |
| ptmove-media-agent-0.3.1-linux-arm64.tar.gz | `2971047a82260bf0ae6ecf93f611887158cb54b137ad7f9a4c95ab9e60612c4b` |

NAS 选择对应架构的镜像加载。ARM64 加载后执行 `docker tag ptmove-media-agent:0.3.1-arm64 ptmove-media-agent:0.3.1`，填写独立 `.env`，再使用 `nexus/proxy/compose.nas.yaml` 及 `up -d --no-build`。NAS 独立源码安装包可在本机按架构构建，包内不含镜像。

详细配置见 [NAS 截图代理](REMOTE_SCREENSHOTS.md)、[API 接入](API_MANAGEMENT.md)。归档不含运行时数据库、环境变量、账号、密钥或媒体；恢复已有实例还需自己的安全数据备份。

## 验证范围

Windows 与 Linux Go 全量测试、前端类型/静态检查及构建通过；31 项 fetch 鉴权回归和 12 项部署保护测试通过。152 上 10 组隔离 API、6 组生产只读 API、11 张 PC/移动页面验证通过。代理真实 FFmpeg/mpv、MediaInfo、预览及一张合成图的真实 Pixhost 上传通过。

ARM64 已构建并验证镜像加载，未在物理 ARM NAS 运行。未安装到真实 NAS，未访问真实 qB/站点执行下载或发种，也未访问受保护的 81 服务器。
