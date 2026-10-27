# PTMove

织梦（zmpt.cc）→ NovaHD（pt.novahd.top）的月度自动转种控制台。独立实现调度、筛选、配额、任务恢复和 NAS 执行端，通过 HTTP API 调用 PTNexus 进行站点发布；未复制或重新分发 PTNexus 源码。

## v0.1.1 功能

- 中文管理页面：规则、候选、批量入队、任务、事件和连接配置。
- 默认每月 1 日 02:00（北京时间），最多 6 个、单个 1–10 GiB、预算 60 GiB，做种人数 ≥3，优先 NovaHD 缺少的作品，其次缺少的版本，同级优先官种。
- 计划默认关闭。启用时若本月计划时间已过，会补执行一次；已有当月计划记录则不重复。手动扫描只产生候选，选择候选后才下载和发布。演示预览明确标记，不连接站点、不写入真实候选或配额。
- NAS 执行端主动领取任务，qB 4.1+/5.x API 下载资源，检查全部文件完成后才发布。PTNexus 复用源站资料并处理媒体信息；目标种子校验与 Tracker 做种确认独立于上传成功。
- 月度名额和资源体积预算原子预留；领取过的任务保留预算。预算按资源文件体积计算，保守计入失败任务的完整预留体积，不是网络流量计费器（协议开销和重传另计）。
- 加密保存连接信息；管理员会话、CSRF 和独立执行端令牌；租约和发布检查点防止超时后盲目重复发布。
- qB 5.2+ 可用原生 API Token（优先使用），无需用户名/密码；旧版继续使用账号登录。遵循 [qB 官方 Bearer 认证规范](https://github.com/qbittorrent/qBittorrent/wiki/API-Key-Authentication-%28%E2%89%A5v5.2.0%29)。

## 部署控制台（152）

```sh
git clone git@gitee.com:star1999/gpt-ptmove.git /opt/gpt-ptmove
cd /opt/gpt-ptmove
python3 scripts/init_deployment.py
docker compose up -d --build
docker compose ps
```

默认只绑定 `127.0.0.1:18120`，不修改系统 Nginx、防火墙或其他项目。Windows 本机运行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\open-console.ps1
```

保持隧道运行，浏览器打开 `http://127.0.0.1:18120`。另一个终端获取初始控制台密码：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\open-console.ps1 -ShowPassword
```

也可直接运行 `ssh -N -L 18120:127.0.0.1:18120 itemkeep-server`。后续有域名可在 HTTPS 反代后开放；配置真实站点账号前，应始终使用 SSH 隧道或 HTTPS。

## 接入 NAS

见 [NAS_SETUP.md](docs/NAS_SETUP.md)。控制台已部署不等于 NAS 已接入：需要 qB 地址/账号、两站 Cookie、NAS 上的 PTNexus URL/账号与对应下载器 ID，并保证 PTNexus 可访问实际媒体路径。只给 qB API 不能让另一个容器直接读取媒体文件。

候选转载限制和页面结构检查失败时跳过；验证码、登录失效、查重未知或发布结果不明时需要人工处理。官种识别使用站点标记和已知织梦组后缀（ZmWeb/ZmAudio/ZmPT），最终仍以站点标记和转载规则为准。当前只实现这两个站的常见 NexusPHP 页面结构；真实账号页面变化需要联调适配。

## 验证

```sh
python -m venv .venv
.venv/bin/pip install -r requirements-dev.txt
.venv/bin/python -m pytest -q
docker build --target test -t ptmove-test:0.1.1 .
docker run --rm --network none ptmove-test:0.1.1
```

测试覆盖配额并发、跨月、租约、发布不确定性、发布后修复、全部文件完成、qB 版本接口和鉴权。模拟适配器测试不代表已经在真实站点发种；实际验证状态见 [VALIDATION.md](docs/VALIDATION.md)。

## 恢复与发布

保留 `.env`、整个 `data/`（特别是加密密钥）和 NAS `worker-state/`。只备份数据库、丢失加密密钥将无法恢复站点凭据和候选数据。停服后做一致性文件备份，或使用 SQLite Backup API；不要只复制正在写入的主数据库文件。

源码和镜像可分别通过 Git 标签及 Docker image ID 追溯。源代码历史发布 ZIP 生成到 `D:\A_GPT\GPT-BackUp\GPT-PTMOVE\YYYY-MM\`，附 SHA256 和恢复说明，不包含真实数据、凭据或 NAS 媒体；不称作完整备份。

PTNexus 是独立上游项目，有自己的用途声明和转种限制。使用其镜像/API 时遵守其声明和站点规则；此项目不绕过限制。
