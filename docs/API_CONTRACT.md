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

## File ownership

Native Go/Vue 0.3.1 API management is documented in [API_MANAGEMENT.md](API_MANAGEMENT.md) and authenticated `/api/v1/openapi.json`. The Python interface descriptions above are retained for the legacy 0.2 service. Native `/api/v1/connection` checks a dedicated machine key without business effects; native administrator key management uses `/api/machine-keys`.

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

- Source candidates must be official releases. For ZmPT, query the official tag (`tag_id=3`) within the configured size bounds and request descending torrent-size order (`sort=5&type=desc`); the adapter must still verify each returned result and apply the rule's exact size and seeder/leech thresholds.
- Never select restricted/no-cross-seed releases, episode-tagged releases, or items already downloaded by this account. A failed or unknown restriction, episode, or download-history check fails closed.
- The target site must affirmatively confirm that the corresponding torrent is absent. Existing torrents, search errors, and unknown duplicate status are all skipped.
- `prefer_official` and `target_absent_priority` remain readable and writable for old clients. They are deprecated, returned for compatibility, and have no effect on the hard filters above.
- Candidate preview exposes eligibility and skip reasons. Candidate `size` is the complete torrent size in bytes; clients should display it in a human-readable unit. A UI toggle or an API patch cannot weaken these checks.
