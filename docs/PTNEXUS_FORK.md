# PTMove Nexus 改版说明

## 版本基线与范围

当前改版基于 PTNexus 仓库 `https://github.com/sqing33/PTNexus` 的 `go` 分支提交 `53eee1fed09121867f956c798c158c13a475d51f`，来源记录见 [`nexus/UPSTREAM.json`](../nexus/UPSTREAM.json)。运行入口是 `nexus/Dockerfile.ptmove` 与 [`nexus/compose.ptmove.yaml`](../nexus/compose.ptmove.yaml)，不是根目录 Python 0.2 的 Docker 配置。

本版保留 PTNexus 的原生 Go + Vue 手动转种流程和设置页，并增加独立自动化、站点声明导入、机器 API。根目录旧 Python 实现及 `docs/` 中原有说明继续作为历史版本材料，不应与本版运行时或数据目录混用。

PTNexus 上游附带其用途和限制声明；使用者需阅读并遵守该声明及 tracker 规则。本 fork 不自行宣称 MIT 或其他许可证，也不表示上游作者背书。上游项目来源和固定提交保留在本仓库记录中。

## Docker 部署与隔离

从 Gitee 获取本项目后，进入 `nexus/` 使用 `compose.ptmove.yaml`。它构建 `Dockerfile.ptmove`，提供 `ptmove-nexus:0.3.0` 服务，将宿主机当前目录 `data/` 单独挂载到容器 `/app/data`，默认绑定主机 `0.0.0.0:18120` 并转发容器 `5275`。可用 `PTMOVE_BIND` 和 `PTMOVE_PORT` 调整绑定地址与端口。`.env.ptmove` 为必需的 Compose 环境文件；建议显式设置高熵 `JWT_SECRET` 并将该文件设为仅管理员可读。

必须为新实例选择独立工作目录、数据目录、环境文件和未占用端口。不要把 Python 0.2 的 SQLite、加密密钥、NAS worker 状态或其他 PTNexus 实例的 `data/` 挂载进来。Compose 对容器限制为 768 MiB 内存和 1 CPU，日志轮转为 10 MiB、保留 3 个文件。容器 healthcheck 调用程序自身检查。

```sh
cd nexus
umask 077
cat > .env.ptmove <<'EOF'
JWT_SECRET=替换为高熵随机值
PTMOVE_BIND=127.0.0.1
PTMOVE_PORT=18120
EOF
mkdir -p data && chmod 700 data
docker compose --env-file .env.ptmove -f compose.ptmove.yaml up -d --build
docker compose --env-file .env.ptmove -f compose.ptmove.yaml ps
```

若要通过公网直接访问，将 `PTMOVE_BIND` 设为 `0.0.0.0` 前，需由部署者配置受控的 HTTPS 反向代理或等效网络保护；Compose 默认端口映射本身不是 TLS。此 API 会处理 tracker 凭据，不应在不可信网络上明文暴露。

2026-10-27 已在 `152.136.236.238:18120` 完成原生改版 Docker 部署、健康与重启验证。原管理员密码保留，自动运行、计划和发布均关闭；旧 0.2 容器和数据保留作回滚。代码提交 `0e160b8` 和 `v0.3.0` 标签已推送 Gitee。正式发行版附件尚未上传、发行版尚未提交：浏览器在文件选择窗口无法确认网址，安全检查中止了该操作。下次从附件上传继续，预定地址为 <https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.0>；当前不能将该地址当作已发布证明。

## 自动化行为与默认值

自动化设置由 `server/internal/service/processing/automation/policy.go` 定义。首次配置的自动运行 (`enabled`)、自动发布 (`auto_publish`) 和月度计划 (`schedule_enabled`) 均为关闭。默认计划值为北京时间每月 1 日 02:00；每月上限 6 个任务、单项 1–10 GiB、总预算 60 GiB、最低做种数 3，优先官种及目标站缺少的作品/版本。配额和体积阈值是资源筛选预算，不是网络流量上限。

手动预览不会自动下载或发布。运行/入队必须由管理员明确启动或有权限的机器 API 请求触发。开启自动发布后，程序仍须证明源站限制状态和目标重复/做种状态；未知或无法验证时应拒绝。执行前必须选中 PTNexus 已配置的原生下载器及完整文件保存目录。

目标站必须在站点声明中显式列出可信 Tracker 主机 `announce_hosts`。留空时只读候选预览仍可用，但自动化启用和下载执行因无法核验目标是否做种而失败。系统不会猜测或从站点 URL 自动推导 Tracker 主机。

## 站点 JSON 导入与兼容边界

站点档案是至多 64 KiB 的严格 JSON 声明，支持 `nexusphp` 引擎、HTTPS 公网 origin、站点角色、PTNexus 映射代号、官组标识、路径模板和有界 CSS selector。schema/template 可在管理员 UI 下载，公开档案可预览、导入、导出。导入结果为 `declared/unverified`；离线格式通过不表示站点 HTTP 页面结构、限制识别、登录或发布已验证。

JSON 中不允许放 Cookie、RSS 或其他秘密。管理员应从 UI 的凭据入口单独配置 Cookie 和 RSS；查询接口只返回是否已配置，不返回凭据明文。Cookie 使用 PTNexus 原生兼容存储；RSS 及机器 API 幂等响应使用独立加密注册表，备份时必须保留 `data/machine-registry.key` 和 `data/internal-api.key`。站点档案需要与 PTNexus 已知的原生站点实现和 publisher 映射匹配；新增普通 JSON 声明不会自动生成新的站点协议或 publisher。没有映射或映射未经实际验证时，不应启用发布。

页面入口：设置/站点配置档案。主要管理员端点（均要求管理员认证）：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/site-profiles/schema` | 下载 JSON Schema |
| GET | `/api/site-profiles/template` | 下载空白模板 |
| POST | `/api/site-profiles/preview` | 离线校验声明 |
| POST | `/api/site-profiles/import` | 导入或按 revision 更新 |
| GET | `/api/site-profiles` | 列出公开档案 |
| GET / PUT | `/api/site-profiles/:id/credentials` | 查询凭据配置状态 / 单独写入凭据 |

## 独立机器 API

管理员在 `/api/machine-keys` 创建、列出和撤销机器密钥；仅创建结果显示一次明文。密钥使用期最长一年，必须有 `read` 范围，并可按需增加 `write` 和 `publish`。访问 `/api/machine-openapi` 可下载当前 OpenAPI 文档；机器 Bearer API 的 OpenAPI 端点是 `/api/v1/openapi.json`。机器 API 只接受独立 Bearer key，不接受浏览器 JWT、Cookie 或 NAS 内部令牌作为回退认证。

机器 API 可读取规则、档案、任务和运行记录，也可更新规则、导入档案、管理档案凭据、预览/扫描、入队及管理任务。写命令要求 `Idempotency-Key`；同一密钥仅对相同 method、path 和规范化 JSON 请求体重放缓存结果，不同请求体冲突。涉及发布的操作要求 `publish`；设置更新在启用自动化/自动发布或存在待执行任务时，需要同时具备 `write` 与 `publish`。读取状态使用 read；扫描和离线声明预览命令需要 write 与幂等键；显式选择候选或入队需要 publish。启用期间修改站点声明或凭据也同时要求 write 与 publish。撤销或过期的 key 不能重放旧响应。

请只经 HTTPS 或等效受保护的可信通道调用机器 API。不要将 token 写进提示词、日志、源代码或命令历史；为自动化客户端单独创建最小权限、短期密钥，并在任务结束后撤销。

## NAS 媒体与 qB 兼容性边界

NAS 上下载完成后，152 还需要读取媒体信息和截图。仅提供 qB Web API 不会把 NAS 文件映射到152。请在 NAS 安装 PTNexus 原生盒子代理并配置下载器的“远程”模式及代理端口，或为容器提供可读取的媒体路径映射。自动化的 qB 请求仍按已配置 Host 直连；原生 use_proxy 设置保留给 MediaInfo、截图和 BDInfo，不会被自动化清除。真实 NAS 代理及路径仍需接入后验证。

若下载器配置使用 Bearer Token，其可用性目前仅基于“API 兼容网关支持该认证方式”的假设。没有对真实 qB 原生 Web API 使用用户 Token 做联调，因此不保证 qB 原生端点、版本或 Token 会被此方式接受。请以实际网关/API 文档为准，先在隔离实例验证连接、添加任务及错误处理；不能将未经验证的 Token 支持描述成已确认功能。

## 数据保全、备份和回滚

同一备份组必须包括固定版本的 Compose、`.env.ptmove`、容器镜像 ID/来源，以及完整 `data/`（数据库、配置、日志及两个实例密钥）。`.env.ptmove` 与加密密钥应按秘密材料保护，不能提交 Git。建议停止该容器后复制全部目录，避免只复制正在写入的数据库文件：

```sh
docker compose --env-file .env.ptmove -f compose.ptmove.yaml stop
tar -czf ptmove-nexus-backup.tar.gz .env.ptmove compose.ptmove.yaml data
docker compose --env-file .env.ptmove -f compose.ptmove.yaml start
```

在升级前留存镜像 ID 和一致性备份。回滚时先停止服务，再恢复与目标旧镜像同时保存的环境文件、数据库和密钥，然后启动并检查 health、登录和配置。不要让旧版本读取升级后的数据库，也不要用其他实例的加密密钥替换 `machine-registry.key`。恢复数据库而不恢复对应密钥，可能导致 RSS 或幂等回复无法解密；更换 JWT_SECRET 也会使既有管理员会话失效。

PTMove fork 禁用了原版在线更新器的远端检查、拉取和覆盖安装路径。更新应使用本仓库确认过的 Docker 镜像/发布包；`/update/pull` 与 `/update/install` 返回冲突并提示通过 Docker 发布更新。此行为避免原版二进制覆盖 fork 的功能和数据结构。

## 验证状态

代码包含自动化策略、站点档案、机器 API 与相关服务的测试；具体执行结果应以当前提交的实际记录为准。已通过 Windows 与 152 Docker/Linux 的完整 Go 测试、前端类型/静态检查与生产构建、10 组 canary API 验证、15 张 canary PC/mobile 截图，以及正式环境 6 组只读 API 和 6 张只读页面截图；重启后账号、密钥与配置不变。真实站点页面/原生 publisher 映射、NAS 媒体代理、qB Token 网关及真实下载/发布仍未联调，计划默认关闭。
