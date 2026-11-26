# PTMove v0.2 legacy API contract

This section documents the legacy Python service only. The native Go STARPT automation API has different rule fields and is documented below; do not use the legacy `official_policy` behavior to configure native STARPT.

v0.2.0 adds JSON site profiles, frozen routes/rules, schema2 worker claims and the scoped AI API; see SITE_PROFILES.md and AI_API.md. v0.1.1 added encrypted secret `qb_api_key`. It takes precedence over qB username/password and uses the official qB 5.2+/WebAPI 2.14.1+ Bearer API mode, never auth/login/logout. NAS workers must now use execution_schema=2; legacy claims return409.

PTMove API service. Python 3.12/FastAPI + SQLite, static HTML/CSS/JS frontend, pull-based NAS worker. Controller deployment uses152 public port18120; use HTTPS or a trusted encrypted channel for external API credentials. All source/target HTTP operations happen on NAS. No real publishing without configured credentials and explicit rule enablement.

## Invariants

- Default source `https://zmpt.cc`, target `https://pt.novahd.top`; configurable registered NexusPHP profiles, snapshots preserve existing task destinations; no tracker mutation of an existing torrent. Restricted/limited/episode/unknown-permission items fail closed.
- Rules disabled initially. Preview and demo never download/publish. A monthly run is unique per Shanghai calendar month. Worker leases are durable and fenced by lease token. At most one active transfer per worker; expired transfers that may have published require reconciliation, never blind repost.
- Reserve a monthly slot and full candidate download size atomically before transfer; maximum 6 slots/60 GiB by default. A published or uncertain task keeps its slot. Failed download keeps full reserved bytes conservatively (partial bytes are never refunded blindly); pre-download cancel may release. Preview no quota impact.
- Authentication: password login, signed HttpOnly session cookie, mutation CSRF header `X-CSRF-Token`; worker bearer separate from admin. Cookie/password/passkey secret fields encrypted at rest and redacted from UI/logs. Site URLs cannot be changed to protected 81 server.

## Stable JSON

Rule keys: `source_site` zmpt, `target_site` novahd, `enabled` false, `day` 1, `hour` 2, `minute` 0, `monthly_limit` 6, `min_size_gib` 1, `max_size_gib` 10, `monthly_budget_gib` 60, `min_seeders` 3, `min_leechers` 0, `candidate_limit` 50, `official_policy` `prefer` (only/prefer/exclude), `categories` ["movie"], `publish_interval_seconds` 600, `max_download_hours` 24, `max_run_hours` 72, `source_max_age_days` 30. Numbers validated server-side. Schedule timezone Asia/Shanghai.

Candidate object: `id`, `source_id`, `title`, `size_bytes`, `seeders`, `leechers`, `official` boolean, `category` movie/tv/other, `detail_url`, `download_url`, `restricted` boolean, `restriction_reason`, `permission_verified` boolean, `target_exists` boolean|null, `target_work_exists` boolean|null, `info_hash` optional, `release_group`, `published_at`, `selected` bool, `reason`, `rank`, `metadata` object. Unknown duplicate result/permission cannot be selected. IDs controller assigned for stored rows.

Task: `id`, `candidate_id`, `candidate` object, `run_id`, `month`, `status` queued/leased/downloading/preparing/publishing/published/seeding/succeeded/failed/needs_review/cancelled, `message`, `published_url`, `created_at`, `updated_at`, `worker_id`, `lease_token` (worker only), `lease_expires_at`, `source_hash`, `target_hash`, `downloaded_bytes`, `publish_attempted` boolean. New transfer success requires target verification, not upload response alone. `publishing` checkpoint set immediately BEFORE external POST. Abandoned publishing becomes needs_review.

Settings keys: `source_cookie`, `target_cookie`, `source_rss_url`, `qb_url`, `qb_username`, `qb_password`, `qb_save_path`, `media_root`, `ptnexus_url`, `ptnexus_username` (default admin), `ptnexus_downloader_id`, `ptnexus_password`. Empty defaults otherwise. UI GET never returns secrets, including RSS URLs, and returns `configured_secrets` map. PUT omitted secrets preserve existing values; empty secret explicitly clears. Service base URLs cannot contain userinfo/query; Legacy source_rss_url host remains zmpt.cc; per-site credentials use the selected site origin. Worker receives full settings through worker endpoint only.

## Admin endpoints

- POST `/api/auth/login` {password} -> {authenticated:true,csrf_token}; GET `/api/auth/session` -> {authenticated,csrf_token}; POST `/api/auth/logout`.
- GET `/health` -> {status:"ok",version:"0.1.0"} no secrets.
- GET `/api/dashboard` -> {version,month,rule,quota:{used,limit,reserved_bytes,budget_bytes,succeeded,published},counts:{candidates,tasks,pending,succeeded,needs_review},workers:[],next_run_at,runs:[]}.
- GET/PUT `/api/rules` -> rule object.
- GET/PUT `/api/settings` -> settings redacted object + configured_secrets map.
- GET `/api/candidates` -> {items:[],total}; POST `/api/preview` {demo:false} -> enqueue scan job, or {demo:true} -> {items:[],demo:true} synthetic policy-only preview with no persistence or quota impact.
- POST `/api/runs` {mode:"manual"} -> creates scan job and run (enabled not required for manual, but still no publish until selected transfers submitted); worker scan is preview by default. Scheduled scans may enqueue eligible transfers only when rule.enabled true.
- GET `/api/tasks` -> {items:[],total}; POST `/api/tasks` {candidate_ids:[]} -> {items:[],queued,skipped:[]}, atomic monthly reservations. POST `/api/tasks/{id}/cancel` refunds only never-claimed jobs; POST `/api/tasks/{id}/retry` only safe stages, published task retry = seed_repair. POST `/api/tasks/{id}/reconcile` {published_url} accepts only a strict target details.php?id=numeric URL for an ambiguous publication and queues seed_repair without changing quota or permitting repost.
- GET `/api/events` -> {items:[]} newest first, severity/info + message + created_at.
- GET `/api/worker-compose` -> {compose:string} with worker token omitted, placeholders; admin downloads with own setup secret separately using POST `/api/worker-token` -> {token}. CSRF on token retrieval.

## Worker endpoints (bearer auth)

- POST `/api/worker/heartbeat` {worker_id,version,capabilities:{}}.
- GET `/api/worker/config` -> {rule,settings,version} (includes secrets, no logging).
- POST `/api/worker/claim` {worker_id,execution_schema:2} -> {job:null|{id,kind:"scan"|"transfer"|"seed_repair",lease_token,payload:{...},lease_expires_at}}. Transfer payload {task,candidate,rule,route,settings}; scan payload {run_id,scheduled,rule,route,settings}. Settings are decrypted for the claimed route only; route and rule are frozen.
- POST `/api/worker/jobs/{id}/renew` {worker_id,lease_token} -> {ok:true}.
- POST `/api/worker/jobs/{id}/checkpoint` {worker_id,lease_token,status,message,published_url?,source_hash?,target_hash?,downloaded_bytes?,publish_attempted?} -> {ok:true}. Refuse stale token; forbid clearing published_url/publish_attempted. Immediately checkpoint publishing before external upload.
- POST `/api/worker/jobs/{id}/complete` {worker_id,lease_token,success,result:{items?:[],status?,message?,published_url?,source_hash?,target_hash?,downloaded_bytes?}} -> {ok:true}. Scan results filtered/ranked and stored; scheduled run may enqueue within quota. Failed post-publish never returns to publish.

## v0.2 administrative and machine interfaces

- Admin site management: GET `/api/sites`, `/api/sites/schema`, `/api/sites/template`, `/api/sites/{id}/export`; POST `/api/sites/preview` profile object; POST `/api/sites/import` {profile,update}; PUT `/api/sites/{id}/credentials` {cookie?,rss_url?}.
- Admin API keys: GET/POST `/api/api-keys`; DELETE `/api/api-keys/{id}`. Creation {name,scopes:[read],expires_days:30} returns {token,key}; token is never returned again. GET `/api/ai-guide` returns copyable integration guide.
- AI Bearer endpoints and scope/idempotency rules are documented in AI_API.md. Authenticated `/api/v1/openapi.json` contains only machine routes. Global `/openapi.json` is disabled.

## Native downloader connection settings

The native administrator submits downloader settings with `POST /api/settings` and uses `POST /api/test_connection` to validate them. Downloader state, version, torrent lists and traffic use the URL in `host` for both modes. `use_proxy=true` also enables remote media processing and tests the selected host name on `proxy_port`. The read-only test validates the downloader API and, when enabled, makes a credentialed media route probe without opening a file or creating a job. `proxy_media_only` is still accepted and retained for configuration compatibility, while `use_proxy` determines the mode. Empty or omitted credential fields retain their saved values unless explicitly cleared.

## File ownership

Native Go/Vue 0.3.1 API management is documented in [API_MANAGEMENT.md](API_MANAGEMENT.md) and authenticated `/api/v1/openapi.json`. The Python interface descriptions above are retained for the legacy 0.2 service. Native `/api/v1/connection` checks a dedicated machine key without business effects; native administrator key management uses `/api/machine-keys`.

The native administrator plugin exposes `GET /api/plugins/tjupt/downloaders`, `POST /api/plugins/tjupt/preview` with `rss_url` and optional `max_gib`, `POST /api/plugins/tjupt/status` with `downloader_id` and `token`, and `POST /api/plugins/tjupt/retire` with those fields plus a single `hash`. Preview and status return `{items: [...]}` and do not mutate downloaders. Retire re-queries H&R and stops exactly one current, scoped qB task through the Feiniu channel. It returns `{ok: true}` on confirmation, HTTP 409 when the status cannot permit retirement, and HTTP 502 when the upstream result is unavailable. Request credentials are ephemeral and omitted from all response bodies.

Native automation settings are returned by `GET /api/plugins/tjupt/automation` and updated by `PUT /api/plugins/tjupt/automation`. The request directly contains `enabled`, `cleanup_mode` (`mark` or `delete`), `downloader_id`, `interval_minutes`, `min_gib`, `max_gib`, `total_limit_gib`, `buffer_hours`, `upload_threshold_kib`, `low_speed_minutes`, and `active_limit`. Preview also accepts optional decimal `min_gib` and `max_gib`. Changing settings under an active lease returns 409. Settings reads and writes do not themselves call the site or downloader.

The plugin page uses the administrator routes under `/api/plugins/tjupt/feed`. `GET` returns the saved settings, `rss_configured` and `hnr_configured` flags, the enabled and running state, and the last-run summary when available. `PUT` saves `downloader_id`, `interval_minutes`, `min_gib`, `max_gib`, `total_limit_gib`, `active_limit`, comma-delimited `tags`, `category` and optional `save_path`. Write-only `rss_url` and `hnr_token` update the stored credentials. Empty or omitted values preserve them. `clear_rss` and `clear_hnr_token` explicitly clear them. Reads and saves omit credentials and do not start a run.

`POST /trial` makes a one-off attempt to add up to one qualifying qB task in a stopped state using the saved settings. It returns counts and a message. `POST /start` enables the regular RSS loop (one minute by default). `POST /stop` disables it. Both return the current settings view. `POST /preview` and `POST /status` read saved settings and reply with `{items: [...]}`. The first reads RSS candidates and the second queries the selected downloader and H&R records. The feed and the older `/automation` control are mutually exclusive.

Feed settings may include `cleanup_enabled` (defaults to false), `upload_threshold_kib` (defaults to 100) and `low_speed_minutes` (defaults to 60). Optional fields omitted by older callers retain their existing values. Enabling cleanup requires the saved H&R credential. Only the formal loop can retire an owned, complete and seeding task. Retirement requires two matching affirmative H&R responses at least 15 minutes apart, a continuous low-speed window, and a final native qB identity and speed check. The qB request removes the task and retains its files. The feed view's last message summarizes retirement outcomes.

For the native feed, `active_limit: 0` removes the task-count cap while `total_limit_gib` and the individual size range continue to apply. A positive value sets an exact count cap. An omitted count retains the saved value. H&R status can return more than 50 tagged tasks, with at most 50 hashes per request to the site.

`POST /api/plugins/tjupt/feed/upload-limit` takes `{"enabled": true}` or `{"enabled": false}` and returns the updated feed view. This independent switch can be changed during the regular RSS loop. The view exposes `settings.use_site_upload_limit` (default false) and read-only `site_upload_limit_mbps` (a positive limit in the MB/s shown in site settings, 0 for unlimited, null when unavailable). The plugin uses the site's existing upload setting when adding new qB tasks, including paused trial adds. Regular settings saves retain this toggle. Existing qB tasks and qB global preferences are unaffected.

The automation response includes redacted `settings`, `rss_configured`, `write_configured`, `delete_configured`, `save_root`, `used_gib`, `last_run_at`, `last_message`, and `items`. Successful qB effects require a receipt for the exact action and hash. Unknown add/delete outcomes retain reservations and hold the execution lease while signed work may still be in flight. The first qualifying cleanup pass only sets the pending tag, even in delete mode.

Backend agent owns `ptmove/api.py`, `ptmove/db.py`, `ptmove/models.py`, `ptmove/policy.py`, `ptmove/security.py`, `ptmove/scheduler.py`, `tests/test_controller.py`, `tests/test_policy.py`. Export FastAPI `app` from api.py, Store from db.py. Policy exports `evaluate_candidates(items, rule)` -> list enriched/ranked dicts.

Frontend agent owns `ptmove/static/*` only. Root owns worker/adapters, packaging, docs, integration tests and CHANGELOG.md; root appends consolidated child records to avoid races.

## STARPT 任务记录中的 NovaHD 资料编辑

以下接口适用于原生 Go 服务的管理员 JWT。记录必须指向已成功发布的 NovaHD 详情，服务端在读取、生成草稿、预览、保存时均重新核对绑定账号归属及编辑权限。

| 方法和路径 | 输入 | 成功结果 |
| --- | --- | --- |
| GET `/api/publish_logs/:id/published_edit` | 发布日志 ID | `revision`、`screenshot_bbcode`、`mediainfo`、`details_url`、可核对时的 `cover_url` |
| POST `/api/publish_logs/:id/published_edit/media` | `screenshots`、`mediainfo` 布尔选择 | 飞牛返回 `screenshot_bbcode` 或 `mediainfo`。部分失败附 `field_errors` |
| POST `/api/publish_logs/:id/published_edit/preview` | `revision` 及变更字段 `screenshot_bbcode`、`mediainfo` | 简介、截图和 MediaInfo 的前后差异及 15 分钟有效的 `confirmation` |
| POST `/api/publish_logs/:id/published_edit/save` | 预览时的同一内容及 `confirmation` | 已回读核验时返回 `status: verified` |

截图只追加 Pixhost 图片并按图片去重。MediaInfo 文本经预览后可补充或更改。生成草稿会上传所选截图至 Pixhost，预览只读取站上资料。实际编辑前保存原文与预期改动，同一目标版本只提交一次。回读不一致或超时时返回 `status: unknown`，打开编辑重新读取站上资料即可核对。不会调用发布队列或改变 qB 做种状态。

更换封面时，预览仅传 `revision` 和 `cover_url`。服务端接受 Pixhost HTTPS 展示页、图片直链和对应缩略图地址，解析并核验可加载的原图。预览返回 `cover_url_before`、`cover_url_after` 和 `confirmation`。保存须传同一份 `revision`、`cover_url`，以及将 `cover_url_after` 原样传入的 `resolved_cover_url` 和 `confirmation`。封面操作需要单独提交，截图和 MediaInfo 字段禁止混传。保存后回读封面和站点表单，只有匹配预览时才返回已验证。

## 转种准备中的 MediaInfo

`POST /api/migrate/prepare_seed_info` 首先沿用草稿的完整媒体报告。报告缺失时，尝试从原简介或缓存简介的 `[quote]` 读取唯一的结构化 MediaInfo。识别成功后规范分行，补齐缺失的 `General` 标题，使用现有媒体校验规则，并在准备日志显示「MediaInfo 从简介提取」。无法确认引用时，使用已配置的飞牛媒体代理。禁转与分集限制继续由转种准备流程执行。

## Native STARPT automation hard filters

This section describes the native Go service and its authenticated `/api/v1` machine API. It supersedes the legacy policy descriptions above for native STARPT automation.

- Source candidates must be official releases. For ZmPT, query the official tag (`tag_id=3`) within the configured size bounds and request ascending torrent-size order (`sort=5&type=asc`). The adapter starts at the first page of that filtered result and advances within the scan limit. It prefers the seeder count from the matching ZmPT listing row and uses the detail page when that count is unavailable. Each result is verified against the exact size and seeder/leech thresholds. Across sources, eligible candidates found by the scan are scheduled in ascending order by verified size, with higher seeder counts first when sizes match.
- Never select restricted/no-cross-seed releases, episode-tagged releases, or items already downloaded by this account. A failed or unknown restriction, episode, or download-history check fails closed.
- For ZmPT as the source, preview and pre-download checks read the completed cross-seed list from ZmPT. A listed target host is a duplicate. A successful list without the target, including an explicit empty list, clears this check. An unloaded widget, failed request, or unrecognized response is pending verification. Other source sites continue to use the target-site check at these stages.
- For new ZmPT runs, `batch_size` sets the maximum downloads per batch (default 20, range 1–100). When current downloads finish, STARPT submits the completed hashes in one IYUU batch using the existing IYUU token. A confirmed target hit skips publishing. Missing or unrecognized results stay in `waiting_batch` for retry. An explicit absence permits media preparation and publication after another source restriction check. Other sources and previously queued jobs retain their existing pre-upload target lookup.
- `duplicate_action` accepts `keep`, `move`, or `delete`. `move` requires `duplicate_directory`, an absolute path on qB, and moves each automation-owned torrent into a task-specific subdirectory. `delete` removes that qB task and preserves the files. Cleanup runs only on confirmed batch duplicates. In-flight moves are verified through qB without issuing another write.
- The automation worker reuses usable screenshots and MediaInfo and fills missing data through the configured pull client. Manual screenshot decisions remain authoritative. After a batch is processed, the run scans again until monthly publish capacity is filled or no eligible candidates remain.
- The Shanghai-month quota is shown as `quota.published` (confirmed new publications) and `quota.pending` (held slots). Duplicate results and definitive failures release slots. Ambiguous uploads hold capacity under the existing no-retry and reconciliation rules. `quota.count` and `quota.bytes` remain legacy accounting fields. Size limits apply per torrent, alongside downloader free-space checks.
- `prefer_official` and `target_absent_priority` remain readable and writable for old clients. They are deprecated, returned for compatibility, and have no effect on the hard filters above.
- Candidate preview exposes eligibility and skip reasons. Candidate `size` is the complete torrent size in bytes; clients should display it in a human-readable unit. A UI toggle or an API patch cannot weaken these checks.
- `GET /api/automation/tasks?active=1` uses the administrator session and returns ongoing tasks for the workbench. `download_progress` is the latest qB fraction from 0 to 1 when available. Other phases use the task state and message.
- Candidate rows include `detail_url` when their source profile provides a valid same-origin HTTP(S) detail address without credentials. The field may also be present on skipped rows so administrators can inspect the source reason.

## 首页任务与草稿媒体补全

`GET /api/migrate/task_monitor` 汇总候选预览、截图、媒体报告、批量获取、下载、发布、BDInfo 和自动化处理。首页和任务抽屉共用列表。候选预览在正在处理的条目与最终可选数量之间分别报告，短期操作结果保留至多 30 分钟和最近 60 条。当前飞牛 HTTPS 客户端回传整批截图结果，本机截图可以报告实际截取和上传的进度。

Web 管理员的 `POST /api/automation/preview` 在扫描成功后保存最近一份候选预览，并返回毫秒时间戳 `previewed_at` 和 `preview_saved`。若更早开始的请求在较新的成功快照之后完成，返回其即时结果与 `preview_saved:false`。`GET /api/automation/preview` 返回 `{success:true,preview:null|{candidates,settings,previewed_at,source_digest?,target_digest?}}`。空结果也是一次成功预览，失败或中断保留之前的成功结果。快照只保存展示资料与站点配置摘要，不登记运行、不占月度额度。带选中候选的 `POST /api/automation/runs` 可以附加 `preview_source_site` 与 `preview_target_site` 绑定站点标识，并附加 `preview_source_digest` 与 `preview_target_digest` 绑定预览时的站点配置。标识或配置已变更时返回 409。入队依然按当前规则复查。机器 API 的预览按原有只读协议返回即时结果。

`POST /api/migrate/refresh_mediainfo_async` 传入 `fill_missing: true` 时只为当前草稿返回报告，先复用完整报告或从简介提取，再尝试下载器与本地分析。此模式只返回结果供草稿回填。缺少 BDInfo 的蓝光原盘仍可使用原有的 MediaInfo「重新获取」入口。

`POST /api/migrate/maintenance/media_fill` 使用 Web 管理员认证。请求体为 `{"seeds":[{"hash":"…","torrent_id":"…","site_name":"…","downloader_id":"…"}]}`，下载器标识可选。每批最多 100 条，重复标识合并。返回 202 和 `task_id`、`task_key`、`total`，用 `task_key` 从任务监控匹配本次批次。同一时间只运行一批，重复提交返回 409。

批量补全只修改选中记录缺失的媒体字段。已有可用截图和媒体报告保留。MediaInfo 优先从简介提取，必要时核对相同哈希的已完成 qB 文件。新增视频截图须取得五张互异的有效 Pixhost 图片才会保存。截图和媒体报告各自检查并发写入，各自保存。此接口不会获取源站种子文件、添加 qB 任务或发布。任务进度为当前服务进程内的短期结果，已落库的项目在重启后仍可读取。

维护详情保存可在 `POST /api/migrate/update_db_seed_info` 附带 `maintenance_media_base`，包含打开草稿时的 `screenshots`、`screenshot_review_status` 和 `mediainfo`。未在草稿改动的媒体字段沿用最新数据库值。响应中的 `saved_maintenance_media` 返回最终保存的资料。
