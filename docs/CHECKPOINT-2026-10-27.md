# PTMove 重启恢复断点

状态：应用户要求暂停开发，等待用户重启电脑后明确继续。记录日期为2026-10-27，时区Asia/Shanghai。

## 已完成并运行

- 项目目录：`C:\Users\50578\Desktop\GPT-PTMOVE`。
- Git远端：`git@gitee.com:star1999/gpt-ptmove.git`，分支master；暂停前业务提交为`e4601c3`，本断点会另行提交并推送。
- 当前程序v0.1.1，已发布v0.1.0和v0.1.1；发行版：<https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.1.1>。
- 实际运行在152.136.236.238，SSH别名`itemkeep-server`，目录`/opt/gpt-ptmove`，容器`gpt-ptmove-controller-1`。
- 公网入口：<http://152.136.236.238:18120>，绑定`0.0.0.0:18120`。本机开发服务与SSH转发均已停止，电脑重启不影响152上的Docker。
- 镜像`ptmove:0.1.1`，ID `sha256:074a09af0a2b2dff9de2cc7ab9cfe238e0453dd88b5f3e0c8e448417441c655c`。容器重启策略unless-stopped，数据保存在152项目data目录。
- 已完成月度规则、配额、候选预览、批量任务、qB密码/API Token认证、PTNexus调用、租约与发布恢复。105项测试通过，公网桌面/手机浏览器检查通过。
- 停止前再次检查152容器healthy，health接口返回版本0.1.1；没有改动或重启其他项目。
- 管理员密码不写在断点中，152上可通过`python3 /opt/gpt-ptmove/scripts/show_admin_password.py`由用户读取。不要把密码、Token、Cookie写入日志、Git或归档。

## 尚未完成的真实接入

- 月度自动计划仍关闭。
- 尚未部署用户NAS执行端，尚无A/B站Cookie或真实下载/发布验证。
- qB Token只在内存中进行过已双重确认的版本测试：qB v5.2.1、WebAPI2.15.1；没有增加/删除/暂停种子或修改设置，没有持久化真实Token。
- qB给定域名解析到受保护的81服务器。过去的两轮确认仅覆盖已完成的两次版本GET，不能用于后续盘点、下载、部署或种子操作。恢复时必须先读AGENTS.md并遵守原用户规则，不能通过代理/域名/子代理绕过。

## 最新待开发需求

1. 允许用户上传符合格式的JSON文件来增加站点。
2. 增加便于用户通过AI操作的API。

当前进度：仅完成代码和接口梳理、初步方案分析；尚未实现站点导入、动态站点执行或AI凭据/API。没有新功能代码变更。只读架构子代理`sites_architecture`已因用户暂停要求被中断，不能假定它完成或认可方案。

## 恢复后建议的实现方向（尚未定稿）

- 站点文件采用声明式JSON，提供schema、示例、下载模板、校验预览与导入管理；不执行上传脚本、不把站点登录凭据混在可分享模板中。
- 第一阶段支持现有通用NexusPHP引擎，配置站点ID/名称/HTTPS地址、路径模板、CSS选择器、官组标识、Tracker域名及PTNexus站点映射。增加文件不代表PTNexus自动具备全新站点协议，需要明确显示兼容与待联调状态。
- 保留织梦/Novahd为内置配置，规则新增来源站和目标站选择，现有用户数据兼容。
- 站点配置接入完整扫描、查重、下载、发布、做种和恢复流程；任务冻结站点配置快照，避免上传新模板改变旧任务的发布目标。
- 当前candidates唯一键为source_id，tasks唯一键为source_id/month。多站点需要改为包含来源站、目标站的复合身份，并迁移旧SQLite记录、保留ID/额度/发布证据与任务关联。
- AI REST API可采用`/api/v1`，独立于浏览器会话和NAS执行端的Bearer密钥；密钥哈希保存、可过期/撤销，区分read/write/publish，默认只读。涉及启用自动发布、入队等动作必须有publish权限。
- 为AI入队/扫描请求提供幂等键，保持原子配额和禁止不确定发布后盲目重发；提供OpenAPI和可复制调用示例，响应不回显秘密。
- 所有导入地址及执行仍须保留81保护、来源限制、禁转/限转、查重未知拒绝、完整资源验证和租约检查。

## 关键代码位置

- `ptmove/models.py`：Rule、设置与请求模型；当前没有站点选择字段。
- `ptmove/db.py`：SQLite建表、候选/任务身份、月度额度、租约和恢复。
- `ptmove/policy.py`：来源URL检查目前写死zmpt.cc。
- `ptmove/adapters.py`：SOURCE/TARGET、官组、Tracker解析、目标announce及PTNexus源/目标映射仍有写死值。
- `ptmove/worker.py`：扫描/转种和目标Tracker检查仍固定织梦→NovaHD。
- `ptmove/api.py`：现有浏览器/执行端API；尚无独立AI API密钥管理。
- `ptmove/security.py`：凭据加密/会话/URL与81保护。
- `ptmove/static/*`：中文控制台，后续新增站点管理和AI接口页面。
- `docs/API_CONTRACT.md`、`docs/VALIDATION.md`、`docs/DEPLOYMENT.json`、`CHANGELOG.md`：现有接口和验证记录。

## 接着做

1. 读本断点、AGENTS.md、CHANGELOG末尾和git status，确认没有用户后续改动。
2. 先定站点schema、迁移与任务快照、AI权限/幂等契约，再并行实现。每组文件只安排一名写入者，子代理不得自行派生子代理。
3. 测试新增站点完整执行链、旧库迁移、同编号跨站隔离、配置变更对旧任务的影响、未知限制拒绝、AI权限/撤销/幂等及泄密负例。
4. 延续152 Docker构建/部署验证，保留现有管理员密码、data目录、18120公网入口和其他服务。
5. 所有实际修改追加CHANGELOG；原用户对提交、推送与发布的授权仍在本会话，但81保护不能用历史授权代替两轮确认。

重启后的继续指令示例：**从断点继续开发站点JSON导入和AI API。**
