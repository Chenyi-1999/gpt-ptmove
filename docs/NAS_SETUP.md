# NAS 接入

## 前提

1. NAS 支持 Docker。Linux NAS 可用此仓库的 `compose.nas.yaml`（host 网络）；其他平台需调整网络连接。
2. 已有 qBittorrent 4.1+ 或 5.x，开启 Web UI API，知道其地址、账号、版本与下载保存路径。
3. 在 NAS 部署并配置 PTNexus，添加织梦、NovaHD 和该 qB 下载器。使用官方镜像/项目说明；它是独立依赖，不包含在 PTMove 自有镜像里。
4. PTNexus 读取媒体的容器路径必须与 qB API 返回的保存路径一致，或使用 PTNexus 自身支持的路径映射。挂载对应 NAS 目录并验证截图和 MediaInfo 功能。

## 连接 152

无 HTTPS 域名时，在 NAS 宿主机建立到控制台的 SSH 隧道：

```sh
ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
  -L 127.0.0.1:18120:127.0.0.1:18120 <NAS上自行配置的SSH用户>@152.136.236.238
```

为 NAS 单独配置 SSH 认证和仅允许所需端口转发的账户。不复制本机现有 SSH 私钥；不要将 SSH 私钥写入项目或镜像。此版本不自动创建服务器 SSH 账户。以后使用可信 HTTPS 时，可改用 HTTPS URL，无需此隧道。

## 启动执行端

从 Gitee 拉取相同版本后，在 NAS 项目目录准备 `.env.nas`：

```dotenv
PTMOVE_CONTROLLER_URL=http://127.0.0.1:18120
PTMOVE_WORKER_TOKEN=在控制台连接页按需获取执行端令牌
PTMOVE_WORKER_ID=nas-1
```

```sh
mkdir -p worker-state
chown 10001:10001 worker-state
chmod 700 worker-state
chmod 600 .env.nas
docker compose -f compose.nas.yaml up -d --build
```

镜像可以在 NAS 从源码构建；152 生成的现成镜像为 linux/amd64，ARM NAS 应在本机构建匹配架构。不要给执行端容器 Docker socket、宿主机 root 或 privileged 权限。

控制台连接页填写两站 Cookie、RSS（可选）、qB URL/账号/保存路径、PTNexus URL/账号、PTNexus 中该 qB 的下载器 ID。秘密字段留空保持原值，需要清除时使用清除操作。控制台不会回显已保存 Cookie/密码。

## 首次验证顺序

1. 控制台看到 NAS 在线；月度计划保持关闭。
2. 手动扫描，检查候选的分类、体积、人数、官种、查重和限制标签。解析不到的字段不会猜测。
3. 选择一个允许转载的小资源入队，在 qB 核对实际下载和文件清单。
4. 检查 PTNexus 元数据/截图，再确认 NovaHD 发布链接、目标 qB 种子和 Tracker 工作状态。
5. 单种通过后测试批量，再启用月度计划。

源站页面、API、RSS、Cookie 和 PTNexus 实际版本可能影响联调。没有账号或连通条件时，不应把模拟测试标为真实发种成功。

## 故障恢复

- 下载失败：资源不自动删除，预算保留；允许安全重试，使用已有 qB 任务继续下载。
- 已发布但加种失败：重试按钮只修复目标做种，禁止再次发布。
- 发布请求超时且没有确定链接：标记待核实，自动重试和跨月重新入队均被阻止。管理员在 NovaHD 核实确已发布后，在任务页填入对应详情链接；系统只安排做种修复并验证文件指纹，没有“忽略保护强制重发”按钮。
- 同一 infohash：仅将经过验证的目标站 announce 加入同一资源的 qB 任务，保留源 Tracker。不会向其他站泄露该 announce。
- 旧月份未发布任务过期：不转移旧月份名额，保留旧预算；需要当前月份重新选种。已经发布的任务可以继续修复做种。
- NAS 重启：保留 worker-state 中的身份和发布日志，控制台租约自动恢复安全阶段。不要清理 journal 来绕过不确定状态。
