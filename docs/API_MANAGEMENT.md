# API 管理与 AI 接入

STARPT 从 0.3.1 起在 **设置 → API 管理**（`/settings/api`）集中管理 AI 和脚本使用的接口。旧 `/settings/ai` 地址继续可用。根目录 Python 0.2 的接口与当前 Go 服务不同；当前接入以本页和在线 OpenAPI 为准。

## 首次接入

1. 登录管理页面，进入 API 管理，创建具名密钥，例如“日常 AI 管理”。先选只读；需要修改配置时选读取与写入，需要执行下载、转种时再授予发布权限。密钥有到期时间，可随时撤销。
2. 创建后立即保存密钥；服务端仅保存哈希，关闭窗口后无法找回。管理页面中的连接测试只向当前部署的 `/api/v1/connection` 发送只读请求，不会连接 NAS、访问站点或创建任务。
3. 下载 **AI 接入包** 和 **OpenAPI 文档**。接入包包含地址、认证方式和操作说明，使用密钥占位符，不含真实密钥、网页登录令牌或站点凭据。
4. 向 AI 提供接入包或本页，将专用密钥保存在调用工具的凭据配置或进程环境变量 `PTMOVE_API_KEY` 中。不要将真实密钥加入源码、共享文档或日志。

当前部署的 API 根地址为 `http://152.136.236.238:18120/api/v1`。远程调用应通过 HTTPS、VPN 或受保护的 SSH 隧道；管理页面上的地址编辑只影响说明和接入包，连接测试始终访问当前页面所属服务，避免把密钥发送到其他主机。

## 认证与连接验证

所有机器请求使用 `Authorization: Bearer <machine-key>`。网页登录 JWT、Cookie 和下载器令牌不能用于该接口，也不能用机器密钥登录管理页面。

`GET /api/v1/connection` 返回当前密钥的公开 ID、名称、权限和到期时间，以及接口版本、管理范围和 OpenAPI 位置。这个接口不启动扫描、截图、下载或发布，不改变业务数据。

```sh
# PTMOVE_API_KEY 由凭据配置注入进程；以下命令不要替换为写死的真实密钥。
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${PTMOVE_API_KEY}" \
  http://152.136.236.238:18120/api/v1/connection
```

OpenAPI 可在管理页面使用管理员会话下载，或用机器密钥读取 `/api/v1/openapi.json`。文档中的路径已经包含 `/api/v1`，调用工具拼接地址时不要重复添加此前缀。

## 可管理的内容

| 能力 | 路径 | 权限 |
| --- | --- | --- |
| 验证身份、读取接口合同 | `GET /api/v1/connection`、`GET /api/v1/openapi.json` | read |
| 读取总览、规则、站点、任务、运行记录 | `GET /api/v1/dashboard`、`/rules`、`/sites`、`/tasks`、`/runs` | read |
| 修改自动化配置 | `PUT /api/v1/rules` | write；启用执行或存在待执行任务时还需要 publish |
| 校验及导入站点 JSON、修改站点凭据 | `POST /api/v1/sites/preview`、`/sites/import`；`PUT /api/v1/sites/{id}/credentials` | write；影响已启用或待执行任务时还需要 publish |
| 预览资源、取消任务 | `POST /api/v1/scans`、`/tasks/{id}/cancel` | write |
| 运行、入队、符合条件的重试和核实发布结果 | `POST /api/v1/runs`、`/tasks`、`/tasks/{id}/retry`、`/tasks/{id}/reconcile` | publish |

read、write、publish 独立检查。publish 不自动授予 write；需要全面管理时显式选择三个权限。机器 API 不提供 SSH、Docker 管理，也不返回现有站点密码或 Cookie 明文。下载器及媒体代理的安装配置仍由管理员在设置页完成。

所有写命令携带 `Idempotency-Key`，为每次新操作生成唯一值，8–128 个字母、数字或 `_.:-`。重试同一操作使用原幂等键和相同请求体。幂等键不能用来绕过撤销、过期或权限检查。发生不确定的发种结果时先核实，不要重新发种。

## 常见结果

- `200`：只读连接通过；仍须按实际操作检查权限和任务条件。
- `401`：密钥错误、过期或已撤销；也可能误用了网页登录令牌。
- `403`：缺少当前操作所需的独立权限。
- `400`：请求格式或幂等键不符合合同。
- `409`：站点版本或幂等请求冲突，需要重新读取并核对。

API 管理页会区分有效、已过期和已撤销密钥。测试框中的密钥只在当前页面内存中短暂使用，离开页面后清除。测试通过不代表真实 NAS、站点协议或发布权限已完成联调。
