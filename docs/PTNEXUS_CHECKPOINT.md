# PTNexus 改版续接记录

当前状态：已从本断点恢复，在 152:18120 完成 0.3.0 Docker 部署与验证；Git 提交及正式发布收尾中。原服务器不可达的历史状态已解除，以下步骤保留作为续接与回滚记录。

- 工作目录：`C:\Users\50578\Desktop\GPT-PTMOVE\.local\worktrees\261027-ptnexus-automation-a901`
- 开发分支：`worktree-lite/261027-ptnexus-automation-a901`，保留未提交文件；基础 master 为 `a7332dc`。
- 上游：PTNexus Go 分支提交 `53eee1fed09121867f956c798c158c13a475d51f`；原生项目位于 `nexus/`，旧 Python 0.2 保留。
- 入口：`nexus/Dockerfile.ptmove`、`nexus/compose.ptmove.yaml`；不能使用根目录旧 Dockerfile 部署新版本。
- 实现：可启停自动下载/转种、原设置页配置、站点 JSON 导入/导出与凭据分离、独立 AI API/OpenAPI。
- 已验证：Windows Go1.23.12 全量测试（count=1）、Vue 类型检查、新增界面十文件 Oxlint/ESLint、Vite 生产编译、Linux/amd64 CGO=0 交叉编译（只编译未运行）、六项执行一致性缺陷独立复查。Vite 仍提示原共享 chunk 偏大。没有启动本地应用服务器。
- 未验证：152 Docker/Linux 运行、浏览器交互、真实 NAS/站点发布、qB Token 网关兼容性。81 服务器不在本次操作授权范围。

## 服务器恢复后的顺序

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
