# 下载器端远程补截图

0.3.1 提供独立的 Linux NAS Docker 媒体代理，复用 PTNexus 原生截图流程。媒体留在 NAS，152 只发送截图请求并接收 Pixhost 图片地址及 BBCode。支持 amd64 和 arm64 构建；32 位 ARM 不在此版本范围。

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

## 在 PTMove 中配置

进入 **设置 → 下载器**，选择对应下载器：

1. 启用“远程”，填写媒体代理端口，默认 `19090`。
2. 填入与 NAS 相同的独立“代理 Token”。留空保存会保留已有 Token；明确勾选清除才会删除。查询配置只返回是否已配置。
3. 开启“仅媒体代理”。qB 的列表、流量和发种前检查继续通过自己的 API 连接，代理负责截图、媒体信息和文件存在检查。
4. 保存后，使用已完整下载的视频测试原生截图预览，再启用真正的自动转种计划。

旧配置没有 `proxy_media_only` 时沿用原代理行为；新版媒体代理不提供 qB 转发接口，使用它时必须选“仅媒体代理”。

## 权限和能力范围

- 必须配置独立 Token 与媒体目录白名单，否则代理拒绝启动。媒体请求只允许白名单内的真实路径，拒绝目录穿越、符号链接越界及请求字段混淆。
- 容器以 UID/GID 65534 运行；NAS 应给予该身份读取媒体的权限。没有 Docker socket、特权模式或主机网络要求。
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
