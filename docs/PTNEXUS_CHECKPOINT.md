# PTNexus 改版续接记录

当前状态（2026-10-27 23:47，Asia/Shanghai）：152:18120 已运行并验证 0.3.0；代码和标签已推送。正式 Gitee 发行版尚未提交，停在附件上传前的文件选择窗口。浏览器工具无法确认该窗口对应的网址，安全检查中止了操作，本轮不得改用其他浏览器控制方式绕过。

- 主工作目录：`C:\Users\50578\Desktop\GPT-PTMOVE`，分支 `master`。原功能 worktree 已合并，不能继续使用旧分支作为断点。
- 功能提交：`0e160b8a1c1803adc3110cff6d4b333294691889`；master 和附注标签 `v0.3.0` 已推送并核验。后续只补充发布记录，不移动标签。
- 上游：PTNexus Go 分支提交 `53eee1fed09121867f956c798c158c13a475d51f`；原生项目位于 `nexus/`，旧 Python 0.2 保留。
- 入口：`nexus/Dockerfile.ptmove`、`nexus/compose.ptmove.yaml`；不能使用根目录旧 Dockerfile 部署新版本。
- 实现：可启停自动下载/转种、原设置页配置、站点 JSON 导入/导出与凭据分离、独立 AI API/OpenAPI。
- 已验证：Windows Go 全量测试；152 Docker/Linux 全量 Go 测试及程序构建；Vue 类型、完整静态检查和生产构建；10 组隔离 API 测试及 15 张 PC/mobile 页面截图；生产 6 组只读 API 及 6 张只读截图；重启后账号、密钥和配置持久性。浏览器控制台错误为 0。六项执行一致性缺陷已独立复查。Vite 仍提示原共享 chunk 偏大。没有启动本地应用服务器。
- 线上镜像：`ptmove-nexus:0.3.0`，ID `sha256:915180c7b9981e19cf48de50275d2812c9824f10e0936b4e21c7204b639c12c7`；容器 `ptmove-nexus-ptmove-nexus-1`；独立目录 `/opt/gpt-ptmove-nexus`。旧 `/opt/gpt-ptmove` 容器已停止并保留。自动化、计划和发布均关闭，原管理密码保留。
- 临时验证项目 `ptmove-nexus-canary` 已关闭；仅其 `canary-data` 和 `.env.canary` 已删除，18121 不再提供该服务，正式数据保留。
- 未验证：真实 NAS/站点下载和发布、qB Token 网关及 NAS 媒体代理兼容性。81 服务器不在本次操作授权范围。

## 下次直接续接

1. 用户关闭系统文件选择窗口并在新一轮回复后，重新读取浏览器控制文档；仅继续 Edge 中 Gitee 新发行版页面。标题、说明及已存在的 `v0.3.0` 标签已填好；先核实页面现状，不重复创建标签或提交发行版。不要操作另一个受保护 qB 标签页。
2. 附件位于 `D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\staging\gitee-v0.3.0-parts\`，上传 `PTMove-Nexus-v0.3.0.part01.zip` 至 `part05.zip` 共五个文件，每个小于 100 MiB。使用浏览器文档支持的文件上传 API，避免无法绑定网址的原生文件选择窗口。五个附件均完成后按已有用户授权提交，核验正式页面和五个下载链接。
3. 分片还原：全部解压至同一目录后运行 `python3 join_release_parts.py`，或在 Linux 拼接 `PTMove-Nexus-v0.3.0.zip.???`。完整历史发布 ZIP 大小 379295851 字节，SHA256 `85ed96c129ebcd9bbe9b52d385b377ebcc2607e91df710ba0b8ce86cafda9e73`。已实际还原并验证 ZIP CRC、所有清单文件哈希和上游符号链接模式。
4. 完整源码和 Docker 历史发布包为 `D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\GPT-PTMOVE__2026-10-27_233335__历史发布__Nexus-v0.3.0.zip`。Docker tar SHA256 `7873108de81bd084a58335bd135ecebaa19ed3f5049a553b24af675269908af3`，本机与 152 一致。没有运行时数据库、账号、密钥或媒体。旧快照保留，21:42 的源码快照漏原生入口，不能独立重建；最终发布包已修正。
5. 正式发行版创建成功后，再追加 CHANGELOG 和发布状态，提交并推送记录；保留 `v0.3.0` 原指向不变。无需因文档记录重建或重启已验证镜像。预定发行版地址：<https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.0>，目前尚未发布。

## 已执行的部署顺序（历史参考，不要重复执行）

1. `ssh itemkeep-server` 检查 `/opt/gpt-ptmove-nexus` 的项目状态。先通过挂载来源识别本任务之前启动的 Go/Node 构建容器，确认结果或仅停止这些容器；不得按镜像名批量停止其他构建。两个旧 SSH 构建会话曾失去响应，完成状态未知。
2. 重新同步最终源码；之前已传的 `nexus-source-dev.tar` 早于最终修复，不能直接部署。Docker 上下文必须排除 `.env*`、`data/`、缓存及密钥。低并发构建，控制资源，保留其他服务。
3. 在 `/opt/gpt-ptmove-nexus` 构建 `ptmove-nexus:0.3.0`，使用新 Dockerfile；确认 Go 全量 Linux 测试和前端构建。BDInfo helper 由脚本按固定提交下载并核对 Git blob 身份。镜像不启动原版在线更新器。
4. 使用独立 canary 数据目录和 18121 端口验证：健康/登录/原手动界面、设置开关、JSON 导入更新/CAS/导出、凭据保留清除、AI 权限/过期撤销/幂等、桌面与移动布局。自动计划与发布默认关闭，不填真实站点/NAS凭据执行任务。
5. 原管理密码只在 152 内存读取；可向容器 `server --initialize-admin` 的标准输入传入用户名/密码 JSON，在新数据目录生成 bcrypt 哈希。该命令拒绝覆盖已有账号，不能把密码写进命令参数或日志。新实例保存 `machine-registry.key`、`internal-api.key`，环境中保持稳定 `JWT_SECRET`。
6. 保留 `/opt/gpt-ptmove` 旧版本、环境和数据的一致性备份；检查是否有需单独导出的用户档案。禁止让新 Go 服务直接读取旧 Python SQLite。确认 canary 通过后，只停止旧 `gpt-ptmove-controller-1` 服务并将新实例切到 18120；失败恢复旧容器。
7. 验证公开 `http://152.136.236.238:18120/`、重启持久性和默认开关，记录镜像 ID/版本/验证限制。关闭并清理仅本任务的 canary 数据。
8. 追加 CHANGELOG，按现有用户授权提交、合并并推送 `git@gitee.com:star1999/gpt-ptmove.git`，建立真实验证后的发布标签及 Gitee Release。源码和 Docker 镜像历史发布包保存到 D 盘归档根目录并校验；若附件超限，按可恢复的分片方式交付。未部署验证前不创建“已完成”正式 Release。

任何目标实际解析为 `81.70.24.5` 的操作（含 qB 域名、代理或只读查看）仍必须按用户规则逐轮取得两次明确确认；历史版本查询确认不能扩展到下载或发种。

本地归档入口为 `python scripts/package_nexus.py`；默认只生成源码快照，必须带实际 Docker 镜像和 `--release` 才生成历史发布包。归档目录：`D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\`。最新快照位置和 SHA256 以归档 INDEX 与后续 CHANGELOG 记录为准。
