# 编写和导入站点配置

STARPT 的站点档案是一个 UTF-8 JSON 文件，每个文件描述一个站点，最大 64 KiB。当前支持通过站内路径和 CSS 选择器读取 NexusPHP 页面。目标站发布仍由 STARPT 已支持的站点发布适配器负责。

## 让 AI 生成配置

将可复制的提示词、配置模板、JSON Schema 和下列资料交给 AI。若部署版本与仓库不同，以「设置 → 站点配置档案」下载的模板和 Schema 为准。示例使用虚构域名，用于展示来源站的路径和选择器写法。[AI 提示词](sites/AI_PROMPT.md) · [配置模板](sites/template.json) · [JSON Schema](sites/schema.json) · [示例配置](sites/example.json)

| 准备的资料 | 需要保留的内容 |
| --- | --- |
| 站点身份 | 站点名称、HTTPS 域名、作为来源或目标使用。已有 STARPT 站点映射时，提供实际使用的映射代号。 |
| 种子列表 | 列表 URL 的路径、包含一行种子的 HTML、种子详情链接和列表容器。 |
| 种子详情 | 标题、简介、种子下载链接、标签、做种人数与下载人数所在元素。若页面有官种标志或明确允许／禁止转载的标志，提供对应 HTML。 |
| 目标站搜索 | 如果作为目标使用，分别提供有结果与明确无结果的搜索页 HTML，以及搜索 URL 的路径和查询参数名称。 |
| Tracker | 提供站点认可的精确 Tracker 主机名。站点网页域名与 Tracker 主机名可能不同。 |

优先从浏览器「查看网页源代码」或网络面板里的 HTML 响应复制所需片段，保留相关标签、类名和属性。只分享脱敏片段，移除 Cookie、账号信息、RSS 地址、下载链接中的密钥、Token 和 passkey。Tracker 只提供主机名。

## 字段说明

| 字段 | 写法 |
| --- | --- |
| `schema_version` / `engine` | 使用 `1` 和 `"nexusphp"`。 |
| `id` / `name` | `id` 唯一，以小写字母开头，最多 64 个字符，只用小写字母、数字、下划线和连字符。更新时保留原 ID。`name` 填站点显示名称。 |
| `base_url` | 填小写域名的公网 HTTPS 源地址，例如 `https://tracker.example.org`。只允许默认 HTTPS 端口，去掉路径、查询串、URL 片段及用户信息。 |
| `roles` | `["source"]` 为来源站，`["target"]` 为目标站，也可同时填写两项。 |
| `ptnexus_site` | 兼容字段，填写当前 STARPT 实例已有的站点映射代号。映射未知时填 `null` 或省略。实际使用为发布目标时，需要 STARPT 已具备该站点的原生发布映射。 |
| `official_groups` | 已确认的官组后缀，例如标题以 `-ExampleWEB` 结尾时填写 `["ExampleWEB"]`。缺少资料时可填空数组。 |
| `announce_hosts` | 列出已确认的精确 Tracker 主机名，例如 `["announce.example.org"]`。每项只填域名，去掉协议、通配符、端口、路径和参数。目标站留空时仅可进行只读候选预览，自动化启用和下载执行会被拒绝。 |
| `paths` / `selectors` | 描述页面路径及 HTML 元素。只使用 Schema 列出的属性。 |

### 页面路径

所有路径以单个 `/` 开头，且与 `base_url` 同源。以下示例展示常见写法，请以实际页面地址为准。

| 字段 | 示例 | 占位符 |
| --- | --- | --- |
| `listing` | `/torrents.php` | 无 |
| `detail` | `/details.php?id={id}` | 恰好一个 `{id}` |
| `download` | `/download.php?id={id}` | 恰好一个 `{id}` |
| `search` | `/torrents.php?search={query}` | 恰好一个 `{query}` |

STARPT 的 NexusPHP 搜索会附加 `search_area`（IMDb 为 `4`，标题为 `0`）和 `search_mode=0`。请提供支持这些搜索参数语义的站点页面。下载路径只描述请求地址，Cookie 等登录凭据在导入后填写。

### HTML 选择器

推荐简单选择器，如 `table#torrenttable`、`a[href*="details.php?id="]` 或 `#torrent-info .seeders`。可使用标签、ID、类名、属性、后代和直接子元素。解析器读取服务器返回的 HTML，依赖 JavaScript 加载的区域、`:has()`、`:nth-child()` 等复杂写法不能靠填配置解决。配置里的双引号按 JSON 规则转义。

| 字段 | 应指向的元素 |
| --- | --- |
| `table`、`detail_link` | 种子列表容器、容器中的种子详情链接。 |
| `title`、`description`、`download_link`、`tags` | 详情页的标题、简介、下载链接和标签元素。 |
| `seeders`、`leechers`、`peers` | 优先用 `seeders`、`leechers` 分别指向做种和下载人数。`peers` 可以作为做种人数选择器的回退，只有能定位到实际做种人数时才填写。 |
| `official` | 详情页中明确标记官种的元素。 |
| `restriction_allowed`、`restriction_denied` | 详情页中分别明确允许或禁止转载的标记。通用标题、整段简介或「未看到禁转」都不应作为允许证据。禁转信号优先。 |
| `search_results`、`search_empty` | 搜索结果行或详情链接、站点明确给出的无结果提示。两个选择器要互斥，避免匹配到登录页、公共容器或加载占位。 |

可选选择器没有可靠依据时直接省略。模板里的基础选择器是程序默认值，采用前要用实际的 HTML 核对。转载限制或重复状态无法确认时，自动化会跳过该候选。

## 导入和检查

1. 把 AI 生成的单个 JSON 对象保存为 UTF-8 的 `站点代号.json`。导入内容中去掉 Markdown 围栏、注释、凭据和 `revision`、`digest`、`health`、`builtin` 等由服务端生成的字段。
2. 打开「设置 → 站点配置档案 → 导入配置」，点「选择 JSON 文件」或粘贴正文，随后点「校验并预览」。核对 JSON 中的站点 ID，以及预览中的名称、域名和角色，再确认导入。
3. 在站点行点「凭据」，单独填写 Cookie 和同源 RSS 地址。编辑凭据时留空保留原值，需要清除时勾选对应选项。
4. `declared/unverified` 表示已登记的配置声明。先使用只读候选预览核对实际解析结果，包括资源链接、人数、标签、转载限制和目标站查重，再决定是否启用自动化。
5. 更新时在现有站点行点「编辑」，校验并预览后勾选「明确更新现有配置」，再点「确认更新」。保留原 ID、源地址和映射代号。更新后重新扫描候选，旧任务仍使用创建时的配置快照。

常见预览错误包括 `invalid or unknown profile fields`（字段或类型有误）、`missing path placeholder`（路径缺少正确的占位符）、`invalid CSS selector`（选择器格式有误）和 `capability_unsupported`（站点映射未配置或不可用）。若 AI 具备项目文件访问权，把错误原文和相应的脱敏 HTML 一起交给它修订。
