# 下载器端远程补截图

0.3.1 提供独立的 Linux NAS Docker 媒体代理，调用 STARPT 客户端执行截图流程。媒体留在 NAS，152 只发送截图请求并接收 Pixhost 图片地址及 BBCode。支持 amd64 和 arm64 构建；32 位 ARM 不在此版本范围。

流程与替换规则概览见 [截图替换设计说明（离线 HTML）](SCREENSHOT_REPLACEMENT_DESIGN.html)。

## 工作方式

抓取资料时，系统先校验原有截图。已有至少三张可用图片时直接保留；缺图或失效时请求下载器端代理处理视频。默认生成五张截图，代理直接上传到 Pixhost，再把全尺寸图片地址返回给主程序。可用的旧图片会与新图片合并去重。

字幕不能确定、截图失败或数量不足时，任务保留为待确认；自动转种不会绕过它继续发种。可以在原生资料页面补图并确认，然后重试自动化任务。已确认的截图会复用，避免再次抓取、截图和上传。上传前还会读取当前截图记录，防止排队期间的修改被旧数据覆盖。

图床固定为 Pixhost 官方 HTTPS API。NAS 临时截图在任务结束后清理；Pixhost 上的图片为公共图床内容，不等于会自动过期的私人临时文件。接口说明见 [Pixhost 官方文档](https://pixhost.to/api/index.html)。

## 在 NAS 上安装

安装包目录为 `nexus/proxy/`，包含 `Dockerfile.nas`、`compose.nas.yaml`、`.env.example` 和原生 Go 源码。

```sh
cd nexus/proxy
cp .env.example .env
# 在 .env 中填入本机媒体目录、JSON 白名单和随机 Token。
# 可用 openssl rand -hex 32 生成独立 Token；不要使用 qB 或网页登录密码。
docker compose --env-file .env -f compose.nas.yaml up -d --build
docker compose --env-file .env -f compose.nas.yaml ps
```

例如 qB 看到的视频路径为 `/volume1/downloads/Movie/Movie.mkv`，则设置：

```dotenv
PTMOVE_MEDIA_SOURCE=/volume1/downloads
PTMOVE_MEDIA_ROOTS='["/volume1/downloads"]'
# PTMOVE_PROXY_TOKEN 必须自行生成并填入，至少 32 字符。
```

Compose 会把该绝对目录以相同路径只读挂载进代理。若 qB 自身也运行在 Docker 中，以 **qB 返回的 save_path** 为准，代理必须看到相同路径；主机实际目录不同的，应相应调整 bind source，保留容器 target 和白名单路径一致。不要修改正在使用的 qB 数据目录来迎合示例。

默认绑定 NAS 的 `127.0.0.1:19090`，适合本机、VPN 或受保护的隧道。跨机器访问需设置受信任的绑定地址及网络通道。控制器目前使用下载器主机名和代理端口连接；如果 qB 域名只提供一个反向代理入口，需要另行安排可达的媒体代理端口或隧道。

## 在 STARPT 中配置

进入 **设置 → 下载器**，选择对应下载器。

- **直连下载器**通过「下载器地址」连接 qBittorrent WebUI 或 Transmission RPC。例如，同机 qB 可填其 WebUI 地址（常用端口 `8080`）。媒体处理使用容器能访问的媒体目录。
- **远程截图与 MediaInfo**继续通过「下载器地址」查列表、状态和流量，同时把截图、MediaInfo 和所需文件检查交给远程客户端。

「媒体代理端口」和「下载器地址」使用相同主机名，各自连接自己的服务。直接连接独立媒体代理时填代理的实际映射端口，默认 `19090`。远程客户端主动领单时，STARPT 默认填 Hub 内部媒体端口 `29090`。远程客户端连接的 HTTPS 领单端口（默认 `28443`）填在远程客户端的服务端地址里。

远程模式需填「媒体代理 Token」。留空保存会保留已有令牌，明确勾选清除才会删除。保存后可使用「测试连接」分别核对下载器 API 和媒体服务，再按需用已完成下载的视频验证截图预览。

现有 `use_proxy`、媒体端口和令牌可以继续使用。旧全功能选项已移除，启用远程媒体时，下载器版本、列表和流量统一经「下载器地址」对应的 API 获取。

## 权限和能力范围

- 必须配置独立 Token 与媒体目录白名单，否则代理拒绝启动。媒体请求只允许白名单内的真实路径，拒绝目录穿越、符号链接越界及请求字段混淆。
- 镜像默认以 UID/GID 65534 运行。对于只有文件所有者可读的下载目录，可在 Compose 中指定与 qB 相同的普通用户 UID/GID，并以只读方式挂载媒体目录。没有 Docker socket、特权模式或主机网络要求。
- 媒体目录只读，临时目录使用有容量上限的 tmpfs；同一时间只处理一个媒体请求。
- 支持常规视频截图、字幕检查、预览、选定时间截图、MediaInfo 和文件检查。这个小容器不挂载 ISO、不运行 BDInfo，也不提供任意命令执行或文件下载。
- 默认生成五张；实际成功数量会返回。截图不足不会被自动发布流程当作完整成功。

## 架构与构建

在 amd64 或 arm64 NAS 上运行 Compose 会按本机平台构建。跨平台示例：

如果使用已经打包的 Docker 镜像，先加载与 NAS 架构对应的归档，再用 `--no-build` 启动。ARM64 归档带独立标签，需要先改为 Compose 使用的本机标签：

amd64 NAS：

```sh
docker load -i ptmove-media-agent-0.3.1-linux-amd64.tar.gz
```

arm64 NAS：

```sh
docker load -i ptmove-media-agent-0.3.1-linux-arm64.tar.gz
docker tag ptmove-media-agent:0.3.1-arm64 ptmove-media-agent:0.3.1
```

完成对应架构的加载并编辑 `.env` 后启动：

```sh
docker compose --env-file .env -f compose.nas.yaml up -d --no-build
```

```sh
docker buildx build --platform linux/amd64 -f Dockerfile.nas --load -t ptmove-media-agent:0.3.1 .
docker buildx build --platform linux/arm64 -f Dockerfile.nas --load -t ptmove-media-agent:0.3.1-arm64 .
```

跨平台构建包含 Debian 媒体工具，需要原生 ARM 构建节点或 BuildKit/QEMU 支持。具体已完成的镜像、运行测试和验证限制见 CHANGELOG 与部署记录；不能用交叉编译成功替代真实 NAS 的路径、权限、网络和解码验证。

## 旧 SSH 部署的验证记录

2026-11-18，152 主程序通过 Docker 容器内的回环地址连接飞牛的 qB 和截图代理。专用 SSH 密钥只能访问飞牛本机的两个服务。飞牛代理使用与 qB 相同的普通用户身份，按 qB 的路径只读挂载 `/keep` 和 `/temp`。在管理页面的「设置 → 下载器」中可对「飞牛刷流 qB」执行连接测试。

用“重复”标签下的真实电影测试时，152 管理接口返回了五张预览图。以没有截图的模拟来源资料走原生后台修复流程，隔离草稿保存了五张 Pixhost 原图。五张图片均在 152 完整解码为 1920×1080。该片未检出可用字幕，草稿保留为待确认。另一个单集样本命中既有分集限制，流程跳过补图。两个样本均未向站点发种。

## 远程客户端通过 HTTPS 主动领取任务

领单客户端可部署在保存媒体的 NAS 或远程 Linux 服务器。客户端向 STARPT 服务端发起 HTTPS 请求，在本机查询下载器、调用媒体代理并回传结果。正式图片由客户端上传 Pixhost。以下沿用现有 152 服务端和飞牛客户端的部署作为示例。

- **152** 运行 `nexus/proxy/pull/compose.hub.yaml`，并加入 PTMove 的 Docker 网络。默认只向公网发布 TLS 端口 `28443`。PTMove 通过 Docker 内网的 `http://ptmove-pull-hub:28380` 查询 qB，媒体代理端口填 `29090`，选择「远程截图与 MediaInfo」。下载器的 API Key 与代理 Token 都填 Hub 的 `controller.token`，飞牛侧的真实 qB Token 放在飞牛。
- **飞牛** 运行 `nexus/proxy/pull/compose.agent.yaml`。领单容器用宿主网络访问现有的 `127.0.0.1:8080` qB 和 `127.0.0.1:19090` 媒体代理。保留现有媒体容器的只读挂载。领单容器只需要 TLS 信任证书、领单 Token 和两项本机服务凭据。
- **密钥与证书**。Hub 的目录由 `PTMOVE_PULL_HUB_SECRETS` 指定，文件为 `server.crt`、`server.key`、`agent.token`、`controller.token`。飞牛的目录由 `PTMOVE_PULL_AGENT_SECRETS` 指定，文件为 `controller.crt`（用于校验 152 的证书）、同一 `agent.token`、`qb.token` 和 `media.token`。两个容器默认使用 UID 65534，挂载文件应允许该用户读取。飞牛的 `PTMOVE_PULL_URL` 为例如 `https://152.136.236.238:28443`，TLS 证书需要包含该 IP 或实际域名。
- **任务边界**。Hub 提供 `POST /v1/claim` 和 `POST /v1/result`，qB 查询和截图分别领取。结果以任务 ID 和租约标识确认，网络重试只回传同一结果。Hub 重启时，尚未完成的请求会失败，已领取的截图任务不会由 Hub 自动重放。扫描通过独立的只读入口查询 qB。既有发种后添加种子流程可提交受限的种子表单。该通道不提供通用下载管理、删除和种子导出接口。媒体队列正在生成正式截图时，连接探针可能需要等当前任务结束，qB 查询使用独立队列。

镜像从 `nexus/proxy/pull` 构建，两个服务使用同一镜像。 客户端可叠加 `compose.agent-admin.yaml` 保留管理页及其连接配置，现有部署继续使用原绑定地址和状态卷。上线前先用该下载器完成连接测试和远程预览，确认截图草稿的写回后再停止旧连接。

### 远程文件查询

「做种检索 → 本地文件扫描」复用同一个 HTTPS 领单连接。领单客户端在飞牛本机读取未筛选的 qB 引用清单，仅回传任务哈希、保存路径和内容路径。媒体代理读取 `PTMOVE_MEDIA_ROOTS` 白名单内的顶层目录，回传缺失名称及孤立文件的名称和体积。STARPT 不需要挂载飞牛的下载目录。

启用这项功能需要同时更新 STARPT、领单 Hub、飞牛领单客户端和媒体代理。原有 qB 与媒体代理端口及凭据配置继续适用。共用同一媒体端口的下载器一起参与引用核对，发现孤立候选后再复核一次 qB 清单。

该功能核验顶层文件或任务内容目录。单目录请求最多 64 KiB，目录最多 20,000 项，差异结果最多 2 MiB。目录不可读、引用发生变化、无法确定文件归属或达到限制时，页面会标明未核验或失败。
