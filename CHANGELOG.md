# CHANGELOG

## PTM-20261027-001 — 2026-10-27 (Asia/Shanghai) — root

- Purpose: initialize the empty Gitee repository and fix the v0.1 controller/NAS API and safety invariants before parallel implementation.
- Files: AGENTS.md, docs/API_CONTRACT.md, CHANGELOG.md.
- Behavior: documented monthly limits, preview isolation, durable leases, publish recovery, local-only deployment and encrypted credentials. No running application yet.
- Checks: git ls-remote returned empty repository; clone succeeded. SSH alias resolved to 152.136.236.238; Docker 29.7.2 / Compose v5.5.0 available, 25 GiB free. Existing project services inventoried only.
- Remaining: implementation, tests, image build/deployment and release. No station credentials or NAS connection supplied; actual tracker publishing cannot yet be verified.
- Git: not committed or pushed. Deployment: not performed. No 81 server access.

## PTM-20261027-002 — 2026-10-27 12:58:06 +08:00 — root/controller, integrated by root

- Purpose/files: implement ptmove/{api,db,models,policy,security,scheduler}.py and controller/policy tests.
- Behavior: validated editable monthly rules, encrypted settings/candidates, signed sessions and CSRF, atomic reservations, durable fenced leases, monthly unique scans, cross-month publication barrier, claimed-task nonrefundable budgets, manual URL reconciliation restricted to seed repair. Schedules default off; missed current-month schedules catch up once on enable.
- Checks: root full final suite 96 passed locally and 96 in isolated Linux Docker; controller agent targeted regressions independently passed. Root inspected interfaces and verified complete executor integration. Independent read-only reviewer findings were fixed with regressions.
- Remaining: real NAS credentials, tracker login/page parsing and real publishing unverified. Git not yet committed/pushed at this entry. Controller deployed healthy on 152; no 81 access.

## PTM-20261027-003 — 2026-10-27 12:58:06 +08:00 — root/dashboard, integrated by root

- Purpose/files: ptmove/static/* and scripts/browser_qa.cjs; usable Chinese console and browser verification.
- Behavior: login, overview, rule forms, real candidate review, isolated demo, task cancel/safe retry/seed repair/reconciliation, secret-preserving settings and Compose download. Historic worker heartbeat expires after 90 seconds. Root fixed JSON double-decode in Compose download, schedule catch-up copy, favicon and small text.
- Checks: JavaScript syntax passed; both local and actual 152 SSH-tunneled browser runs passed login/demo isolation/Compose download/labels and responsive checks at 1440,768,390; each retained 8 PNGs under ignored output/playwright. Zero console errors, inspected screenshots, no page overflow, no checked visible text below 12px.
- Remaining: actual NAS onboarding not performed. Git not yet committed/pushed; UI deployed on 152 and browser verified. No screenshots contain account secrets.

## PTM-20261027-004 — 2026-10-27 12:58:06 +08:00 — root

- Purpose/files: ptmove/adapters.py, ptmove/worker.py, tests/test_adapters.py, tests/test_worker.py, tests/test_schedule_display.py.
- Behavior: independent ZMPT/NovaHD metadata adapters, manifest/piece-fingerprint duplicate checks, qB v4/v5 APIs and complete-file verification, PTNexus API preparation/publish, NAS persistent journal, pre-POST checkpoint and lease recheck, URL-only publish receipt, target Tracker verification and same-hash tracker installation. No blind retry after uncertain upload. Configured endpoints resolving to protected 81 are blocked before requests.
- Checks: adapter/executor mocks passed as part of full 96-test suite. Success, post-publish failure/repair, uncertain publish, lease loss during checkpoint, image-only restrictions, identical infohash and partial-file cases covered. No real site requests in tests.
- Remaining: actual site HTML, qB/PTNexus versions, media mounts and real transfer unverified. Git not yet committed/pushed. Worker packaged but not deployed to user's NAS; controller only on 152.

## PTM-20261027-005 — 2026-10-27 12:58:06 +08:00 — root

- Purpose/files: Dockerfile, compose*.yaml, requirements*.txt/lock, builddeps/README.md, scripts/*.py/ps1, README.md, docs/*, .gitignore/.dockerignore.
- Behavior: isolated non-root read-only controller deployment in /opt/gpt-ptmove on 152, loopback18120 with SSH access, NAS worker packaging, encrypted-state restore instructions, verified D-drive release packager. No credentials in Git/image/archive.
- Checks: first online Docker build was cancelled after slow PyPI downloads (~13 KiB/s); target Linux wheels staged on D and offline build succeeded on 152. Runtime healthy, auth/worker auth/restart rule+session persistence/unchanged quota passed. Image linux/amd64 ID sha256:1ed71625ab408c77655b334b0e6bc7254a9f910bd8369d3e4c77ba2f8c90ec12, about36MiB RAM. Existing services preserved; no global Docker/DNS/Nginx changes. Docker image exported on 152 with SHA256 recorded in docs/DEPLOYMENT.json.
- Remaining: source/image release ZIP and Gitee push/tag pending at this record. Real NAS/trackers not validated; schedule remains disabled. The initial entry001 omitted its minute; exact historical minute was not recorded and is not invented here.
- Git: not yet committed/pushed. Deployment: successful on152, loopback only. No81 server access.

## PTM-20261027-006 — 2026-10-27 13:03:03 +08:00 — root

- Purpose/files: .gitattributes and release coordination records; finalize cross-platform line endings and record verified deployment before tagging.
- Checks: initial implementation commit 5a1ff4d435353ca2103cf3df80d01fbd703ff362 pushed successfully to origin/master. Runtime and actual browser checks remain as recorded in005/VALIDATION.md; no application code changed after those checks. Docker export copied directly to D staging and SHA256 matched 3e79a6e8e667d5fca72437e26b429724bc2cb721a8c6f0fa634dd14025ee104b.
- Remaining: final release archive/tag and Gitee release page pending. User provided a remote qB endpoint; DNS-only investigation showed protected81, so no HTTP/API operation was performed and separate authorization rounds are pending. Credential was not written to files/log/Git. NAS-local qB configuration remains the intended execution route.
- Git: implementation committed/pushed; this coordination entry not yet committed at writing. Deployment: healthy on152. Real NAS/tracker actions not validated and monthly schedule off.

## PTM-20261027-007 — 2026-10-27 13:16:19 +08:00 — root

- Purpose/files: record completion of the base v0.1.0 release and D-drive source/image archive.
- Checks/results: tag v0.1.0 points to2f5f959ac011a4454913d5d1e1b8ddaf3498a495, pushed successfully. Gitee release https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.1.0 visibly created with source/image ZIP attachment. D archive GPT-PTMOVE__2026-10-27_130306__历史发布__v0.1.0.zip includes source + linux/amd64 Docker image, restore instructions and SHA256 manifest; archive verification passed, overall SHA256 b7e2c7c9b7d2a9c0e33039d0084a818caa15c589e5a56dc431f757a69b071d8e. INDEX.jsonl updated; no credentials/live data/media included and not a complete backup.
- Git: base release committed/pushed/tagged. Deployment: base release verified on152 then superseded by Token patch below. Remaining: true NAS/tracker publishing unverified. No81 API access.

## PTM-20261027-008 — 2026-10-27 13:16:19 +08:00 — root/qb_token_support + root/dashboard, integrated/reviewed by root

- Purpose/files: adapters.py, worker.py, models.py, security.py, static UI, version metadata/Compose/API, packaging/smoke/browser scripts, tests/test_qb_token.py and adjusted controller/executor tests; README, NAS/API/release/deployment/validation docs.
- Behavior: v0.1.1 adds encrypted qb_api_key, Bearer API mode for qB5.2+/WebAPI2.14.1+, Token precedence without login/logout or password fallback, key-only NAS worker support, UI preserve/clear and no echo. Version/packager metadata synchronized; base tag/history preserved. Protected81 policy unchanged.
- Checks: root complete local105 tests passed, isolated LinuxDocker105 passed, independent reviewer8 Token tests passed with no material issue; actual152 and local browser runs each passed8 screenshots and Token form behavior. Updated image sha256:074a09af0a2b2dff9de2cc7ab9cfe238e0453dd88b5f3e0c8e448417441c655c deployed healthy; smoke restart persistence/auth/worker auth/defaults+quota preservation passed; exported image SHA256556e66f74ecd4ba77822a5adb1058c6a988c4df0bc1a88f4161a5d84c014263a matched D-drive copy.
- Remaining: new version Git commit/tag/Gitee release/archive pending at writing. User's actual qB Token not persisted or used; only first of two protected-server confirmations received. No real NAS/trackers download or publish validation, schedule remains off.
- Git: prior base committed/pushed; this patch not yet committed/pushed at writing. Deployment: v0.1.1 on152, loopback18120, unrelated services preserved.

## PTM-20261027-009 — 2026-10-27 13:22:04 +08:00 — root

- Purpose/files: final linked release/deployment record in CHANGELOG.md; server Git metadata initialized in /opt/gpt-ptmove using a locally created bundle (no SSH private key copied), deployment-only artifacts excluded via local .git/info/exclude.
- Checks/results: patch commit0674307 and annotated v0.1.1 tag pushed to Gitee. Formal release https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.1.1 visibly shows latest, commit0674307 and downloadable source/image archive. D archive GPT-PTMOVE__2026-10-27_131625__历史发布__v0.1.1.zip passed ZIP CRC and per-file SHA256 checks; overall SHA256a35779466c3a8c8459d06f691d3c63f8e67acc3d5ce9a5c7ae1a3606e05a44c4; archive index updated, both versions retained. Includes source, linux/amd64 image, restore notes and checksum manifest; excludes credentials/live DB/media and is not a complete backup.
- Deployment: /opt/gpt-ptmove Git and tracked files clean atreleasecommit0674307; runtime0.1.1 healthy, only127.0.0.1:18120. Local temporary QA servers stopped, user-facing SSH tunnel retained. Final metadata record will be synchronized without rebuilding unchanged application code.
- Remaining: onlyfirst protected81 confirmation received; no qB HTTP/API request made and actual Token not persisted. Real NAS execution, A/B login, resource download and publishing remain unverified; schedules off. Second confirmation is pending for the exact authentication/version-read-only plan and does not authorize torrent changes.
- Git: code/release tags committed and pushed; this metadata-only entry not yet committed at writing. No application behavior change from this record.

## PTM-20261027-010 — 2026-10-27 13:35:07 +08:00 — root/public_bind, integrated by root

- Purpose/files: compose.yaml, README.md, docs/DEPLOYMENT.json, docs/VALIDATION.md and CHANGELOG.md; follow explicit user request for direct 152 access on a port in10000-50000 and no local service.
- Behavior: controller Docker publication changed from127.0.0.1 to0.0.0.0:18120; public URL http://152.136.236.238:18120. Existing administrator authentication, secrets, data volume, hardening and ptmove:0.1.1 image preserved. Only18120 exposed; no whole-range opening or unrelated service/network changes. Local QA services and SSH tunnel stopped, verified no listeners on18120/18121/18122 on this machine. Administrator console password displayed only to the requesting user, not recorded here.
- Checks/results: public health200/version0.1.1, loginpage200, anonymousdashboard401 from a no-proxy external client. Actual public browser login/demoisolation/Compose download/responsive checks passed with8screenshots and zero console/overflow/label/tinytext findings;152container healthy on0.0.0.0:18120. Configuration-only change; unchanged application image was not rebuilt and unchanged105test suite was not needlessly repeated.
- Protected-scope verification: two independent user confirmations received for the exact qB authentication/version-only plan. Actual execution made exactly2BearerGETs to app/version and app/webapiVersion; both200, results v5.2.1 and2.15.1. No torrent/settings mutations, no persistent actualToken storage, and no broader81 authorization inferred. Preliminary SSH input-channel attempts failed before contactingqB, then an echo-disabled stdin-only probe completed; no token printed or written.
- Remaining: real NAS worker deployment and A/B download/publish validation unperformed; monthly schedule off. The completed81 read-only plan does not authorize future torrent operations. v0.1.1 release artifact remains its original historical snapshot; currentmaster contains the updated public deployment configuration.
- Git: prior releases committed/pushed; this config/docs change not yet committed at writing. Deployment: public access already applied and verified on152; source/metadata synchronization to follow this record.

## PTM-20261027-011 — 2026-10-27 16:43:28 +08:00 — root

- Purpose/files: docs/CHECKPOINT-2026-10-27.md and CHANGELOG.md; user requested a progress report and durable breakpoint before rebooting their PC.
- State: paused the new stationJSON-import/AI-API work after read-only exploration, before any feature implementation. Interrupted read-only sites_architecture child. Recorded completed release/deployment, pending requirements, provisional architecture, file map, migration/auth invariants and exact resume steps; no secrets saved.
- Checks: local and152 Git were clean at e4601c3 before checkpoint;152 Docker remains healthy, public0.0.0.0:18120, health returns0.1.1. No new runtime code changes, tests/build/deploy not rerun. Existing105 passing tests remain the previous release validation, not evidence for the unimplemented features.
- Remaining: JSON schema/registry/import UI/dynamic execution and AI credentials/scopes/idempotent API are not implemented. ActualNAS andA/B publishing still unverified; schedule off. Protected81 prior version-only plan already completed and does not authorize new operations.
- Git: e4601c3 previously pushed; this checkpoint documentation is to be committed/pushed and synchronized to152. Backup: source/Git checkpoint to be written and verified directly on D under the prescribed archive root. Deployment: unchanged, no server/container restart needed; PC reboot will not stop152.

## PTM-20261027-012 — 2026-10-27 16:46:12 +08:00 — root

- Purpose/files: CHANGELOG.md; complete the reboot checkpoint record linked to PTM-20261027-011.
- Git verification: checkpoint commit f7ece5569e4b5e0a513ef8bde39a27324715a5fc is pushed to Gitee master and synchronized to /opt/gpt-ptmove on152; both worktrees were clean.
- Backup: D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\GPT-PTMOVE__2026-10-27_164536__源码快照__重启断点.zip; source and complete Git bundle for f7ece55, excludes runtime data/secrets and Docker image. SHA256 1adb3c29095900d1241d0075c70f005d3347b9d9f6251c30967370b2e485cb84. ZIP CRC and every source/bundle SHA256 verified; Git bundle verified; archive INDEX.jsonl updated. This is a source checkpoint, not a full runtime backup.
- Deployment/checks:152 container healthy on0.0.0.0:18120, image ptmove:0.1.1. No application changes, rebuild, restart or new test run; JSON-site/AI-API implementation remains pending. No81 operation performed.
- Handoff: user-requested pause remains in effect; resume from docs/CHECKPOINT-2026-10-27.md after the user asks to continue. This completion record will be committed, pushed and synchronized without restarting the service.
