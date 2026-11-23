# STARPT

STARPT 是基于 Go 和 Vue 的 PT 种子管理程序，提供手动转种、可关闭的自动下载与转种、站点 JSON 导入和机器 API。仓库中的 Python 0.2 实现及文档用于旧版本维护，不是当前运行入口。

使用者应遵守站点规则和适用法律。

## 功能与安全默认值

- 提供 Go 后端、Vue 管理界面和手动转种流程。
- 自动化运行、定时计划和自动发布默认关闭，分别由设置页中的开关控制。首次配置时先检查站点映射、凭据、下载器和路径，再由管理员明确启用。
- 默认计划为北京时间每月 1 日 02:00，每月最多 6 项，每项 1–10 GiB，月度资源预算 60 GiB。自动化只选官种，并要求目标站确认没有对应种子。
- 站点 JSON 是公开元数据声明，不包含 Cookie、RSS 或 API 密钥。导入时做严格格式校验；`announce_hosts` 必须明确填写，系统不会根据站点域名猜测 Tracker 域名。
- 声明导入不等于站点已经可用。站点需映射到 STARPT 已实现的发布适配器，并验证该映射和真实页面行为；未知限制、查重结果或目标做种状态必须按未通过处理。
- 独立机器 API 密钥提供 `read`、`write`、`publish` 范围，可设到期时间并撤销；写命令使用幂等请求键。一般变更要求 `write`；启用发布、已有待执行任务或明确入队/发布时还要求 `publish`。管理员可下载 `/api/machine-openapi`，机器 API 文档为 `/api/v1/openapi.json`。
- qB Bearer Token 接入仅假设目标 qB API 兼容 Bearer API 网关；本改版没有验证真实 qB 原生 Token 支持，也不保证用户提供的 Token 可直接使用。请按实际网关文档配置并在隔离环境验证。
- API 管理提供密钥权限预设、有效／过期／撤销状态、同源只读连接测试和不含真实凭据的 AI 接入包，见 [API 管理与接入](docs/API_MANAGEMENT.md)。
- NAS 端 Docker 媒体代理支持 Linux amd64／arm64，可提取 MediaInfo 并截帧上传 Pixhost。自动任务补齐的有效资料可以直接进入发布队列，人工标记为待确认的截图仍需审核。 [远程补截图](docs/REMOTE_SCREENSHOTS.md)

## Docker 安装

先准备 Docker Compose 环境，并为本改版选择独立目录。不要复用旧 Python 版的目录、数据库、密钥或 NAS worker 状态目录。

```sh
git clone https://gitee.com/star1999/starpt.git
cd starpt/nexus
```

创建仅供该实例使用的 `.env.ptmove`。至少设置高熵、私有的 `JWT_SECRET`；不要提交该文件。`PTMOVE_BIND` 默认 `0.0.0.0`，若只需本机或反向代理访问，可设为 `127.0.0.1`。端口默认 `18120`。

```sh
umask 077
cat > .env.ptmove <<'EOF'
JWT_SECRET=替换为随机生成的长随机值
PTMOVE_BIND=127.0.0.1
PTMOVE_PORT=18120
EOF
mkdir -p data
chmod 700 data
docker compose --env-file .env.ptmove -f compose.ptmove.yaml up -d --build
docker compose --env-file .env.ptmove -f compose.ptmove.yaml ps
```

首次安装的用户名为 `admin`；临时密码在容器启动日志中显示，首次登录后需修改。152 现有部署通过初始化工具保留原管理员密码。

Compose 只将当前目录的 `data/` 挂载到 `/app/data`，使用容器内 5275 端口，主机端口由 `PTMOVE_PORT` 控制；容器资源上限为 768 MiB 和 1 CPU。请使用独立主机目录与独立端口，避免覆盖 PTMove Python 0.2 或其他服务。该 compose 文件引用 `.env.ptmove`，且不会自动创建管理员以外的站点凭据。

STARPT v1.0.7 提供 Linux AMD64 完整 Docker 镜像和离线 Compose。安装和升级步骤见 [v1.0.7 发行说明](docs/RELEASE-v1.0.7.md)。152 的部署与测试记录见 [DEPLOYMENT.json](docs/DEPLOYMENT.json)，API 管理入口为 `/settings/api`。

## 初次配置

1. 登录 STARPT 设置页，确认管理员登录和手动转种功能正常。
2. 在自动化页面检查初始状态：全局执行、计划执行和自动发布应保持关闭。配置来源站、目标站、下载器 ID、保存路径和筛选规则后，再分别决定是否启用。
3. 在站点配置档案页下载 schema/模板并导入声明。声明仅支持有界的 NexusPHP 页面路径和选择器；导入显示 `declared/unverified`，不是连通性或兼容性证明。
4. 为导入的站点单独录入 Cookie 和 RSS 凭据。JSON 导出不包含凭据；Cookie 使用兼容存储，RSS 及机器 API 幂等回复使用独立加密存储。保管 `data/` 中的 `machine-registry.key`，它与数据库备份必须配套。
5. 确认目标站配置准确的 `announce_hosts`，且对应站点映射到已有原生 publisher。没有可信 Tracker 主机声明时，执行和自动发布会拒绝进行；不推测或自动补全此字段。
6. 通过预览和小范围人工操作验证限制识别、查重和目标站行为。只有验证无误后才按需开启自动化。不要对不确定的上传结果盲目重试。
7. AI/外部客户端使用独立机器密钥，不复用管理员 Cookie、JWT 或 NAS 内部令牌。对接写操作时给每个动作提供唯一的 `Idempotency-Key`；失效或权限不足的结果应停止并由管理员检查。

## 数据备份、升级与回滚

每个实例应单独备份 Compose 文件、`.env.ptmove`、完整 `data/` 和镜像版本/ID。`.env.ptmove` 与 `machine-registry.key` 含敏感材料，应限制访问并加密存放；数据库不能脱离加密密钥单独作为可恢复备份。计划备份时先停止本实例，再一致性复制整个 `data/`：

```sh
docker compose --env-file .env.ptmove -f compose.ptmove.yaml stop
tar -czf ptmove-nexus-backup.tar.gz .env.ptmove compose.ptmove.yaml data
docker compose --env-file .env.ptmove -f compose.ptmove.yaml start
```

升级前保留当前镜像 ID 与上述一致性备份，再切换到明确版本的 Docker 发布包。升级后检查容器健康、管理员登录、站点和密钥状态、任务及配额，再考虑启用计划。

回滚必须将应用镜像、Compose 配置和同一时间点的数据库/密钥备份作为一组恢复，避免新旧 schema 混用。停止本实例，恢复旧镜像与备份的 `.env.ptmove`、`data/`，然后启动并核对健康状态。不要把新版本数据库单独交给旧版本运行。应用镜像及更新包应来自本项目发行页；STARPT 已关闭在线覆盖更新功能，避免远端文件覆盖当前服务。

## 历史版本

仓库根目录原有 Python 0.2 版文档和部分历史记录仍保留，用于旧版维护，不描述当前 Go + Vue 运行时。当前版本说明见 [`docs/STARPT_GUIDE.md`](docs/STARPT_GUIDE.md) 和 [`nexus/server/docs/PROJECT_STRUCTURE.md`](nexus/server/docs/PROJECT_STRUCTURE.md)。
