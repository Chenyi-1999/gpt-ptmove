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
