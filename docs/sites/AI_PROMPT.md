# 让 AI 生成 STARPT 站点配置

将下面的整段提示词复制给 AI，然后附上[配置模板](template.json)、[JSON Schema](schema.json)以及根据[编写说明](../SITE_PROFILES.md)准备的站点资料。方括号内的内容由你替换。

```text
请为 STARPT 生成一个站点配置 JSON，内容必须符合我提供的当前版本 Schema 和模板。根据我提供的脱敏 URL 和服务器实际返回的 HTML 选取路径和选择器。

站点名称：[名称]
站点 ID：[希望使用的唯一小写代号]
HTTPS 网页域名：[例如 https://tracker.example.org]
用途：[来源站、目标站或两者]
STARPT 已有站点映射代号：[代号，未知时填“未确认”]
官网认可的 Tracker 主机名：[只提供域名，未确认就留空]
已知官组后缀：[没有或未确认就留空]
列表、详情、下载和搜索的脱敏地址：[按页面分别提供]
列表 HTML、详情 HTML：[保留内容所需的标签和属性]
目标站有结果与无结果的搜索页 HTML：[作为目标站时提供]

按以下规则生成：
1. 每个文件仅一个 JSON 对象，使用 schema_version 1 和 nexusphp。只写 Schema 定义的字段。已有配置更新时保留原 id、base_url 和 ptnexus_site。
2. paths 为以单个斜杠开头的站内路径。listing 不写占位符，detail 与 download 各恰好使用一次 {id}，search 恰好使用一次 {query}。检查站点是否支持 NexusPHP 的 search_area 和 search_mode 参数语义。
3. 用 HTML 样本核对每个选择器。优先选择稳定的标签、类名、ID 或属性，避免复杂伪类及依赖 JavaScript 的元素。seeders 只统计做种者，leechers 只统计下载者。
4. restriction_allowed 和 restriction_denied 只能对应站点明确的转载标志。search_results 与 search_empty 分别匹配实际结果和明确的无结果提示。没有确认依据的可选选择器直接省略，未知的站点映射填 null，未知的 Tracker 主机不要猜测。
5. 文件不包含 Cookie、账号密码、RSS 链接、passkey、Token、密钥、服务端生成的元数据或脚本。不要请求登录凭据，也不要执行下载、发种或修改站点。
6. 如果缺少必要路径、HTML 资料，或者站点不是支持的 NexusPHP 页面结构，先列出缺失资料或说明无法靠配置支持。资料足够时只输出可直接保存为 .json 的正文，不带 Markdown 围栏、注释或其他文字。
```
