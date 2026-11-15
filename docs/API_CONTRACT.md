# PTMove v0.2 API contract

v0.2.0 adds JSON site profiles, frozen routes/rules, schema2 worker claims and the scoped AI API; see SITE_PROFILES.md and AI_API.md. v0.1.1 added encrypted secret `qb_api_key`. It takes precedence over qB username/password and uses the official qB 5.2+/WebAPI 2.14.1+ Bearer API mode, never auth/login/logout. NAS workers must now use execution_schema=2; legacy claims return409.

New independent implementation; no PTNexus source copied. Python 3.12/FastAPI + SQLite, static HTML/CSS/JS frontend, pull-based NAS worker. Controller deployment uses152 public port18120; use HTTPS or a trusted encrypted channel for external API credentials. All source/target HTTP operations happen on NAS. No real publishing without configured credentials and explicit rule enablement.

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
