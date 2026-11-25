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

## PTM-20261027-013 — 2026-10-27 17:41:56 +08:00 — root/site_core/site_api_ui/ai_tests, reviewed by release_review

- Purpose/files: ptmove/sites.py, models.py, db.py, policy.py, adapters.py, worker.py, api.py, ai.py, static/*; tests/test_sites.py/test_ai.py/test_multisite_worker.py plus updated worker/controller fixtures; compose files, version0.2.0, README, API_CONTRACT, SITE_PROFILES, AI_API, docs/sites, deployment_smoke. Resume authorized siteJSON import and AI API development.
- Behavior: strict offline JSON profiles/schema/preview/import/export, encrypted per-site credentials and dynamic source/target; composite-key SQLite migration preserves IDs/jobs/quota/evidence; run/task rule and route snapshots with stale-candidate fencing and old-worker claim rejection. Machine keys are hash-only, scoped/read-default/expiring/revocable; transactional idempotency reauthorizes cached requests. Browser UI supports sites/keys/guide/OpenAPI. Defaults remain schedule-off.
- Review corrections: generic validation responses exclude raw secrets; fixed custom path/query IDs, frozen-target recovery labels, NexusPHP IMDb search mode, affirmative empty-result checks, mapping/Tracker readiness before reservation/download, API-scope indirect-publish guards and populated v1-only OpenAPI. Runtime transport pins checked DNS to the connection and validates all torrent announces; unsupported webseed/bootstrap endpoints rejected.
- Checks so far: original controller/policy regression and37 profile tests passed;11 AI tests and10 dynamic-route/network tests added; initial full161-test suite passed before final readiness/workercompatibility corrections, final integration run ongoing. Actual tests use isolated temporary data and mock network; no81 access. StaticJS syntax passed. Browser/Docker checks pending next.
- Git: uncommitted/unpushed at writing. Deployment:152 still running0.1.1, no runtime update yet. Existing pre-change source/Git reboot checkpoint retained onD. Next:152 Docker test/build, consistent pre-migration runtime backup, upgrade/browser verification, source/image archive and authorized commit/push/release. Real NAS/trackers remain unverified.

## PTM-20261027-014 — 2026-10-27 17:50:35 +08:00 — root

- Purpose/files: scripts/browser_qa_sites.cjs and static/*; integration/deployment verification and mobile logout fix.
- Checks: final local full suite163passed/one Starlette httpx deprecation; JS syntax and gitdiff whitespace clean after removing trailing blank line.152 offline-wheel Docker test image built and full163tests passed with --network none. Runtime ptmove:0.2.0 built on152; final static-only layer includes visible mobilelogout and clears one-time API/worker token DOM on session expiry.
- Browser progress: isolated gpt-ptmove-qa020 on152:18121 with disposable data/credentials exercisedJSONpreview/import, per-site credentials preserve/clear, source-route save/restore, AI readonly key creation/read/revocation and OpenAPI download. First harness run reached all screenshots then encountered a response-read issue; changed reads to browser APIRequestContext. Next harness reached final logout but existing mobilelogout was hidden; appfix added. Another rerun exposed asynchronous editor replacement in the harness; waits now follow actual completed render. Final full browser run pending.10desktop/mobile screenshots inspected; no actual user secrets in screenshots.
- Isolation: production18120 still0.1.1 at writing. Synthetic profile/keys only in disposable QAdata; no NAS/Tracker/qB request. No81 operation.
- Git: uncommitted/unpushed; release pending. Next: verify browser, stop onlythis controller for consistent encrypted DB backup, preserve existing .env/encryption.key, start0.2.0 and confirm migrated counts/quota/health; archive onD.

## PTM-20261027-015 — 2026-10-27 17:54:55 +08:00 — root

- Purpose/files: docs/DEPLOYMENT.json, VALIDATION.md, CHECKPOINT-2026-10-27.md, RELEASE-v0.2.0.md, scripts/browser_qa_sites.cjs and CHANGELOG; verified deployment/release preparation.
- Result: both152 isolated-mutation and public18120 browser runs passed,10screenshots each; zero console/pageoverflow/label/tinytext findings. JSON upload, credentials preserve/clear, rules, AI readonlykey creation/read/revocation and OpenAPI exercised onlyinQA; production checks kept counts/quota unchanged. Mobilelogout verified and temporary token DOM cleared. QA container and onlyits disposabledata removed;18121 closed.
- Deployment: ptmove:0.2.0 healthy on152:18120, imageID sha256:8db0dc779432fb1f1cfb4d7e6414ade1814a90c087d5f7d00ff12bcdbad267a8. Restart persistence/admin+worker auth passed, original rules restored. SQLite schema0to1 retains candidates0/tasks0/runs1/jobs1, quota0. Deployment .env/encryption.key unchanged; schedule remains off.
- Backup: D database ZIP GPT-PTMOVE__2026-10-27_175152__数据备份__升级0.2.0前数据库.zip SHA2560772f24357bf3b57f38972eb60049c05ddde06d5402226e155a44a08c543e1e3; CRC/perfilehash/SQLite integritycheck passed. Excludesplaintextcredentials/key; restore needs existing152key. Local archiveINDEX updated. Exported Dockerimage SHA25627a12ac309c638765f004f4e876a1a6f8c36332d7a30fcf945bcda792d140edc;D transfer verification ongoing.
- Checks summary:163local and163Linuxnetwork-none tests passed; final static-only mobilelogout layer verified inbrowser. Sharedvisual-runtime scripts unavailable; existing standalone browserQA extended. No81 access, realNAS/download/publish remains unverified.
- Git/release: changes tocommit/push/tagv0.2.0 next; source+image archive and Gitee formalrelease pending at writing.

## PTM-20261027-016 — 2026-10-27 17:57:45 +08:00 — root

- Purpose/files: CHANGELOG.md; linked commit/archive completion record.
- Git: feature/docs commit9d8015d0242925e9254dfab6074222d28d4da328 pushed toGitee master; annotatedv0.2.0 tag pushed.152 trackedfiles/Git synchronized and clean atthatcommit; deployment remainshealthy0.2.0.
- Archive: D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\GPT-PTMOVE__2026-10-27_175604__历史发布__v0.2.0.zip; source+linux/amd64Dockerimage, restore and SHA256 manifests, excludesaccounts/liveDB/media. ZIPCRC andeveryfilehash verified bypackage_release.py; overallSHA25679a2f9bcf51027c0ada007f645e617af16e5d031ee528f6b2f5ce7d7b1b861e5. Dockerarchive onD matches152 SHA25627a12ac309c638765f004f4e876a1a6f8c36332d7a30fcf945bcda792d140edc. ArchiveINDEX updated, historicalversions retained.
- Browser publication: Gitee formalrelease form filled forv0.2.0 andverified52.6MiB archive selected; createaction submitted, finalpublication stillbeingverified. No additional applicationcode changes/testing/restart needed.
- Remaining: formalreleaseconfirmation, finalmetadata commit/push/sync. RealNAS/trackerpublishing remainsunverified, scheduleoff, no81access.

## PTM-20261027-017 — 2026-10-27 17:59:33 +08:00 — root

- Purpose/files: CHANGELOG.md and docs/DEPLOYMENT.json; finalrelease confirmation.
- Gitee: https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.2.0 visibly reports creation successful/latest, taggedcommit9d8015d and downloadable52.6MiB source/DockerZIP withthe verified filename. Browsernavigation observations timedout whileGitee loaded; fresh inventory/state then confirmedsuccess, no duplicatecreation attempted.
- Deployment/validation:152 public18120 healthy at0.2.0, defaultscheduleoff, credentials/data preserved;163local+Linuxoffline tests and20browser screenshots already verified. TemporaryQA18121 removed. No newruntime changes/tests/rebuild/restart fromthis metadata record.
- Git: featurecommit/tag already pushed; linkedrecords016/017 and releaseURL metadata will nowbe committed/pushed and synchronized to152. D release/database archives and indices verified; releaseZIP remains immutable atfeaturetag.
- Remaining limits: realNAS/newsite/PTNexus download/publish verification notperformed; importedprofiles are declarations, HTTP API needs trustedsecurechannel/HTTPS for externalcredentials. No81 operations authorized or performed duringthis featurework.

## PTM-20261027-018 — 2026-10-27 20:52:47 +08:00 — root/nexus_module_architecture

- Purpose/files: importoriginalPTNexus source undernexus/ pinnedgo commit53eee1fed09121867f956c798c158c13a475d51f; UPSTREAM.json recordsprovenance/excludedprebuiltplatformbinaries/wiki. Worktree-lite isolatedbranch261027-ptnexus-automation-a901 createdbeforefeatureedits. ExistingPython0.2source/history preserved. NativeGo optionalautomation +JSONprofiles+machineAPI implementationbegun.
- Invariants/design: nativepublisher/limits reused; separate durableautomationquotas/state/snapshots; guardedatomic handofftonativequeue; removePOST networkretry; partial/unknownpublication quarantined. MachineBearer separatefromJWT, no loopbackadminbypass; origin/DNS/pinnedconnect protectionfor81 installedbeforebackgroundworkers. Nativeconfigmode0600. Rootcoordinatesbootstrap/downloaderToken/transport; workersdisjointautomation,siteAPI,Vue.
- Skills: smart-development, frontend-design-codex andupstreamserver-standards/worktree-lite/wsl-win-notify read. Userexplicitcommit/publishauthorizationcontrolsfinalintegration; retainregressiontests peruserprojectcoverage, donotexecuteupstreamblankettestdeletion. InitialworktreehelperlaunchfailedbecauseminimalGit lackedbashname/PATH/od; sh.exeisGNUbash, explicituniqueID+task-localPATHsucceeded. Sourceinitiallycopieduntrackedthenmovedintoverifiedisolatedworktreebeforefeatureedits. No unrelatedfilesreset/moved.
- Backup: D prechangeZIP GPT-PTMOVE__2026-10-27_204555__修改前备份__基于PTNexus开发前.zip; source+Githistory, CRC+perfilehashverified, overallSHA256d10aae778285a04edf5a0b6e0f1642e49f72ab27271464804038328cf8dc04ea; archiveINDEXupdated. No runtimecredentials/data included.
- Checks/deployment: upstreamsourceinspectioncomplete; nofeatureruntimeverificationyet. Go1.23alpinebuilderpulledon152; separate /opt/gpt-ptmove-nexus builddirectoryprepared. Production18120stillhealthyold0.2.0, no replacementsyet; no81/NAS/qBrequests.
- Git/status: nativeimport+changesuncommittedintheisolatedworktree; no newpush/release. Remaining: implementation,fullGotests/Vuechecks,152Docker/canaryQA, cutoverwithrollbackstatepreservation, authorizedGiteecommit/release.

## PTM-20261027-019 — 2026-10-27 21:40:54 +08:00 — root/nexus_automation/nexus_sites_machine_api/nexus_settings_ui/nexus_execution_review/nexus_fork_docs/go_check_debug

- Purpose/files: native PTNexus import under nexus/ now implements optional durable automation (repository/processing/acquire/migrationflow/publish), strict site JSON registry and native mapping overlays, scoped machine API/OpenAPI/transactional replay, original Vue settings and authenticated SSE bridge. Root wired bootstrap/admin JWT and internal authentication, qB Bearer transport plus write-boundary guard, torrent payload/network checks, 81 DNS/connect fence, fork update metadata, timezone data, Dockerfile.ptmove/compose.ptmove.yaml, pinned BDInfo fetch helper, safe stdin administrator initialization, native backup packager and docs. Python0.2 source/history/runtime data remain separate.
- Behavior: default module/schedule/auto-publish off; monthly6/1–10GiB/60GiB; manual preview no quota/download effects. Attempts/uncertain outcomes durable, no blind upload retry; atomic queue handoff, quota/leases/frozen identities, target whole-payload/tracker verification, explicit announce_hosts needed. JSON contains no credentials; native Cookie compatibility remains plaintext, RSS/replay encrypted. AI read/write/publish independent; configuration writes additionally require publish when active/pending; no JWT/Cookie/NAS fallback. Scans use two short transactions around read-only network work so admin disable/revoke is not blocked.
- Independent review/fixes: native manual/ordinaryqueue ledger bypass, actual HTTP write-boundary stop gate, publish replacing write, pre-upload failures stuck queued_publish, oldest50 starvation, trailing path separators. Added regression coverage; reviewer rechecked all six fixes and found no current second-upload POST path. One taskId-alias concern was explicitly withdrawn after full workflow inspection; no speculative alias change made.
- Checks actual: Windows Go1.23.12 full go test -buildvcs=false -count=1 -timeout=90s ./... PASS; Vue type-check PASS; oxlint ten changed UI files zero warnings/errors; eslint same ten PASS; Vite production compilation PASS (existing large shared chunk warning). No local application server started. Go SDK/mod/build caches on D staging. Initial compiler failure from accidental Transmission apiKey field fixed; concurrent import edits caused one failed graph compile, then all writers stopped before passing final full run. Go process diagnostics did not establish network/SumDB as cause; no unsupported root-cause claim. Linux cross-compile initially lacked cached uuid module, explicit dependency download/retry pending at writing. Python asset-helper syntax check PASS.
- Deployment/connection: separate /opt/gpt-ptmove-nexus prepared and earlier source transferred on152; Go/Node isolated dependency checks were started there but no completion received after SSH/18120 became unresponsive. Source tar first read before SCP completion produced EOF, then re-extracted after complete transfer; current remote copy predates latest fixes and must be replaced before building. No production cutover/restart/cleanup command issued. User independently confirmed152 is inaccessible. Current production health and owned builder state unknown; inspect project-specific mounts only after connectivity returns, preserve unrelated services. No81/NAS/real tracker operations.
- Git/release/archive: feature worktree remains uncommitted; no new push/tag/release and no claim that0.3.0 is deployed. Existing0.2 release/history retained. Prechange D archive018 verified; new source-only checkpoint/archive and final metadata next. Remaining: synchronize final native source, finish152Docker build plus offline Linux tests/canary UI QA, preserve existing administrator login in independent native data, cutover18120 with rollback, then authorized Gitee commit/push/release and verified Docker archive. Real81-hostedNAS operations require fresh applicable double confirmation, not covered by this task.

## PTM-20261027-020 — 2026-10-27T21:44:05.503607+08:00 — root

- Purpose/files: docs/PTNEXUS_CHECKPOINT.md and ignored base .local/PTNEXUS_RESUME.json record exact active worktree, completed validation and remaining152 deployment/release steps. scripts/package_nexus.py and nexus/VERSION add source-only checkpoint vs Docker-release distinction; Docker context excludes live state and credentials. Final UI error handling now displays API error details. No local app server started.
- Checks: full Windows Go tests, Vue typecheck and ten-file lint already passed019; after final UI error-detail change typecheck/eslint/Vite production compile passed. Linux/amd64 CGO_ENABLED=0 cross-compile now PASS after explicitly fetching missing uuid module using pinned go.sum; binary stored only D staging, not executed. Python helper/packager syntax and git diff --check passed. Independent reviewer reconfirmed six fixes, current publisher paths issue at most one uploadPOST and do not redirect-replay.
- Archive: D:\A_GPT\GPT-BackUp\GPT-PTMOVE\2026-10\GPT-PTMOVE__2026-10-27_214233__源码快照__Nexus-v0.3.0.zip; source-only ZIP705files, noDocker/liveDB/credentials/media/SDK/cache, ZIPCRC and every manifest SHA256 verified; overallSHA256 641ed5d83ad7f8a1d6c85c519708dcbdd507c322b1153c75bf8fd8259eacd443. D archiveINDEX appended. Archive captured all source changes through019 and initial checkpoint; this linked020metadata is later. Existing historical archives retained.
- Server: user independently confirmed152 cannot be reached. Local handles for this task's stalled SSH build sessions are being closed; actual remote builder state/completion remains unknown until reconnect. No production cutover/stop/restart, no81 operations.
- Git/release: all feature changes retained uncommitted in isolated branch; no newpush/tag/release. Respect requested order: Docker/canary validation precedes completion commit/release. Remaining work is documented in checkpoint; no claim of deployment success.

## PTM-20261027-021 — 2026-10-27T22:26:47.433152+08:00 — root/nexus_api_qa/nexus_browser_qa/nexus_nas_media_check

- Purpose/files: resume on152 after user confirmed restored connectivity; Dockerfile.ptmove now accepts public registry build args, raises frontend Node heap to1024MiB and aligns Go test/build trimpath. compose.ptmove.yaml accepts isolated runtime env/data; scripts/deploy_native.py preserves existing administrator login via stdin and provides project-scoped canary/cutover/rollback. Added remote-only API/browser QA scripts; QA guards have7 passing offline tests. Three new settings components formatted; JSON editor now takes revision from table metadata instead of credential-free export, and surfaces API error details. Browser QA covers explicit CAS update, one-time key/revoke/OpenAPI, PC/mobile and secret redaction.
- Critical packaging correction: imported upstream server/.gitignore pattern `server` also ignored cmd/server source when imported as untracked files. First server Docker build passed internal package tests but failed missing cmd/server. Anchored binary ignore to /server; all3entry-source files now included. Previous source snapshot641ed5d... verified its listedfiles but omitted native entrypoints and cannot independently rebuild native image; retain it as historical/incomplete snapshot, corrected source archive pending. package_nexus.py now refuses missing required nativeentrypoints.
- NAS compatibility: automationQB no longer rejects native UseProxy media mode; qB automation still directly uses configured Host with guarded transport, while native MediaInfo/screenshot/BDInfo retain original remote box-proxy configuration. Added loopback-only regression proving directAPI+retainedmedia proxy; local targeted testPASS. Read-only code investigation verified NASfiles require native proxy or accessible media mapping, qBAPI alone doesnot exposefiles. RealNAS/81network still unverified and unauthorized.
- Actual build/checks: initial Node Docker build heapOOM134 at512MiB; fixed explicit1024MiB heap, bounded entire sequential build1536MiB RAM/no extra swap/1CPU (first retries0.75CPU). Node typecheck+Vite Docker compilationPASS. Internal Linux Go packages passed prior incomplete-context build; final build now includes CLI+newNAS test and remainsrunning. Earlier SSH wrapper had malformed local shell exit expansion; replaced with Python subprocess/status runner. Source tar28b55207d468d7b2d61306ddc5cad47e26567e3739027e9b06b235c87b387949 includes finalcode and entries. No local appserver. Source/Python/Node syntax and target UItypecheck/eslintPASS.
- Deployment/data: old ptmove0.2.0 remains healthy18120, no cutover yet. No old customsites/APIkeys/workers/candidates/tasks found (counts only inspected, no secrets printed). Online SQLitebackup integrityok; D ZIP GPT-PTMOVE__2026-10-27_222517__数据备份__切换Nexus前数据库.zip SHA25610f2b260c000b41ae91b05c9a4b2bd65cbaed5b996342db54619d5d748492d7f verifiedCRC+perfileSHA+SQLiteintegrity. ScopeoldDBonly, existing .env/encryption.key retained on152; not completebackup. No unrelated service or81 operation.
- Git/release: stilluncommittedworktree; no newpush/tag/release. FinalDocker/canary/PCmobileQA and productioncutover precede authorizedcommit+Giteepublication. Need finalsource/image archive and linkedcompletionrecord.

## PTM-20261027-022 — 2026-10-27T23:28:51.516114+08:00 — root/nexus_ui_debug

- Purpose/files: App.vue now uses local bundled background and waits for authentication before private settings/task-monitor reads, with login/logout lifecycle guards. Original ICO decoded but failed browser IMG rendering; favicon.png preserves identical256x256RGBA pixels, index/App reference it. Added automation switch aria-label and accurate sequential-controller/qB-download concurrency wording. SettingsView scoped direct-child flex-shrink0 fixes real mobile card/alert clipping in newpages. QA harness uses dropdown aria-controls, visible checkbox labels, scoped rows and animation-complete screenshots; secret-masked diagnostics and canary origin fence. No localappserver.
- Build/network: allLinuxGo tests includingCLI andNAS regression passed and servercompiled on152; newVue builds/typechecks passed. DirectGitHub large-file transfer stalled; verified3 pinnedpublic helpers onD thenSCP152, build verifiescachedGitblobIDs again. Added optional builddepscache and package sizeguard. Stopped only identified owned dependency-build containers to switch to cache/Tencent signedAPTmirror; no unrelated services touched. Current finalimage915180c7b9981e19cf48de50275d2812c9824f10e0936b4e21c7204b639c12c7, linuxamd64,371818588bytes. ExportedDockerTARSHA2567873108de81bd084a58335bd135ecebaa19ed3f5049a553b24af675269908af3; D transfer resumedwithSSHcompression, fullDhashpending.
- ActualQA:10canaryAPIgroupsPASS;15PC/mobile screenshotsPASS,3pages×2widths nooverflow/missinglabels/clippedpanels; console/networkerrors0, PNGnaturalWidth256. JSONupload/export/CASupdate, credentialpreserve/clear, one-timeAIkeys/revoke/401/OpenAPI and oldmanualnav exercised in disposabledata only. FullfrontendreadonlyOxlint50files andESLintPASS; targetedchecks afterfinalVuefixesPASS. Allappcodewritersstoppedbeforefinalimagebuild.
- Production: switchedonlyold gpt-ptmove-controller-1 (verifiedcomposelabels) to stopped/retained, deployednew ptmove-nexus service on18120 with independentnative data andpreservedadminpassword. Health0.3.0PASS;6readonlyAPIgroups and6PC/mobile screenshotsPASS, persistentstateunchanged/console0. Dockerrestart preservesJWT/config/env/registrykeys; schedules/module/publishremainoff. No productionfixture/site/key mutations. No81/NAS/realtrackeraccess; truecontentdownload/publishstillunverified. Old0.2image/.env/encryption.key/data retained; consistentDBDbackup021verified.
- Records/packaging: DEPLOYMENT.json andfork/checkpointdocs updated toactualstate; sourcepackagerrequiresnativeentrypoints and preserves2upstreamsymlink modes. Stagedimport retainsupstream intentionalMarkdown/trailingwhitespace baseline; gitdiffcheck onownedchangesPASS; fullimportcheck reportsbaseline whitespace, no massformat/rewrite. Source/private-key/tokenpatternscan714files foundnone. No credentialsinGit/images/release.
- Git/release: featurestagedforfinalreview, commit/push/tag/GiteeRelease andsource+DockerDarchive pending atwriting; authorized orderdeployment-beforepublication satisfied. Need finalarchiveverification, publicationconfirmation, canarycleanup andlinkedrecord.

## PTM-20261027-023 — 2026-10-27T23:48:36.5847571+08:00 — root/release_record_verify

- Purpose/files: docs/DEPLOYMENT.json, docs/PTNEXUS_CHECKPOINT.md, docs/PTNEXUS_FORK.md and ignored base .local/PTNEXUS_RESUME.json updated from old unreachable/uncommitted state to actual deployed/pushed state and precise remaining publication checkpoint. No runtime code or image change.
- Git: feature committed then squash-integrated as 0e160b8a1c1803adc3110cff6d4b333294691889; master pushed. Annotated v0.3.0 pushed, remote tag object f5aa7f45b88bf8cb9befdabf561c4affcd3175cc peels to 0e160b8a1c1803adc3110cff6d4b333294691889. Remote refs checked again. This linked metadata change will be committed/pushed separately without moving the release tag. Original feature worktree cleanup first reported failure; current git worktree list shows only base and this metadata worktree, no feature worktree registration.
- Archive: final D historical release GPT-PTMOVE__2026-10-27_233335__历史发布__Nexus-v0.3.0.zip,379295851bytes,SHA25685ed96c129ebcd9bbe9b52d385b377ebcc2607e91df710ba0b8ce86cafda9e73. Includes source plus verified linux/amd64 Docker tar7873108de81bd084a58335bd135ecebaa19ed3f5049a553b24af675269908af3; native entrypoints and upstream symlink modes present. ZIPCRC/everyfileSHA verified. Five sub100MiB ZIP attachment wrappers prepared for Gitee limit; actual concatenation/reassembly SHA and CRC match original. Independent readonly agent recomputed all five rawchunk hashes and reconstructedZIP hash; PASS. ArchiveINDEX contains archive/distribution records. No liveDB/credentials/media/keys included; all historical snapshots retained.
- Deployment/cleanup:152 production ptmove-nexus-ptmove-nexus-1 still healthy18120, health returns service ptmove-nexus/version0.3.0/upstream53eee1f. Existing account preserved; automation/schedule/publish off. Only project ptmove-nexus-canary stopped/removed; asserted zero canary-labeled containers and exact non-symlink paths before removing /opt/gpt-ptmove-nexus/canary-data and .env.canary. Production data directory preserved; no unrelated service or81/NAS operation.
- Publication blocker: Gitee release form has existing v0.3.0 tag/title/notes filled, but no attachments uploaded and no Create submitted. Opening native file chooser caused Computer Use to stop because it could not confidently determine browser URL on Windows for policy enforcement. No alternate UI/API route used to bypass stop. Formal Gitee Release remains unpublished; user needs close chooser and start a fresh turn, then continue via documented browser-bound file upload API and verify five downloadable attachments. Local artifacts retained.
- Checks: prior Linux/Windows Go/frontend/API/UI/restart validation unchanged022. This record changes metadata only; JSON parsing and gitdiff whitespace check pending immediately after writing, no extra rebuild/restart warranted. Windows blocked notification and final metadata push next. True NAS/qB gateway/actual download and publishing remain unverified,81 protected.
- Record validation: DEPLOYMENT.json parsed and git diff --check passed; ignored resume state saved. Public deployment metadata will be copied to /opt/gpt-ptmove-nexus/deployment.json; this has no runtime settings or secrets.

## PTM-20261115-024 — 2026-11-15T23:40:45.2411953+08:00 — root/api_management_gap/api_management_backend/api_management_ui/remote_screenshot_module, reviewed by api_management_review

- Purpose/files: native0.3.1 API management enhancement in AISettings/router/nav, machine key service/handler/OpenAPI, deployment helper/version/compose, docs/API_MANAGEMENT.md/API_CONTRACT.md and remote-only browser QA. User additionally requested missing screenshot repair on downloader/NAS with Pixhost; implementing authenticated restricted-media native box proxy Docker package, per-downloader independent proxy token/media-only switch and preservation/redaction, existing repair-flow integration. Root coordinates this log; backend/UI/proxy writers own disjointfiles. No local application server.
- Design/invariants: reuse existing machine keys/scopes/replay instead of parallel API; GET connection returns only public identity/scopes/expiry and never business/network calls; AI package has placeholders only. Probe hardcoded same-origin/no cookies/redirects/credential persistence. Existing use_proxy also affected qB torrent listing; new explicit media-only mode preserves direct qB controls while media requests use authenticated agent. Agent requires token+canonical allowed roots, read-only mounts and bounded media processing; no broad NAS access. Preserve existing subtitle/manual-review gates and old valid screenshots; no blind publication retry. No81/NAS/qB/realtracker operations authorized or performed.
- Actual early checks: focused machine handler/migrationflow Go tests PASS (worker); Python native upgrade/smoke guard12tests PASS; Node QA syntax PASS. Independent review reproduced expired-key display and ambient Compose-data redirection; fixed reactive expiry clock and deterministic compose env/data/port/image interpolation, review recheck PASS. Frontend formatting/build/browser and finalfullGo/proxyDocker/capture checks still pending.
- Backup: D prechange source-only GPT-PTMOVE__2026-11-15_232609__源码快照__Nexus-v0.3.0.zip,716members,SHA256b3cd0070ad1ef331cc6f18f85d0bac73f7d6cc853b37287947e7bc5472470958; ZIPCRC+everyfilemanifestSHA verified and archiveINDEX appended. No liveDB/credentials/media/SDK/cache. Historical0.3.0 release archive retained.
- Server/Git:152 production0.3.0 healthy18120, unchanged atwriting; isolated worktree261115-api-management-b101 based844c9a3. Go1.25-alpine pulled on152 for agent build, no production/container changes. Source changes uncommitted/unpushed; no0.3.1tag/release. Prior0.3.0formalGiteeRelease remained pending browserattachment upload, not represented as published. Remaining: finish screenshotagent/tests/review, full152Docker+canaryAPI/PCmobileQA/syntheticcapture, consistentnativeDBbackup beforeproject-onlyupgrade, archive and authorizedcommit/push. RealNASinstall needs useraddress/architecture/path and applicable81doubleconfirmation iftargetprotected.

## PTM-20261116-025 — 2026-11-16T00:24:27.2731098+08:00 — root/api_management_backend/api_management_ui/remote_screenshot_module/automation_screenshot_gate/machine_probe_auth_fix, independently reviewed by api_management_review

- Purpose/files: completes024 APIsettings+nativekeyconnection/status/OpenAPI, NASmediaagent Docker/Compose and perdownloader token/media-only config, strictmedia pathschemas, missing-screenshot repair, canonical publicationgates, repo atomic preparedsnapshot+attempt callback, fetchauth separation and tests. Rootdocs README/API_MANAGEMENT/REMOTE_SCREENSHOTS/API_CONTRACT/fork and .dockerignoreupdated; rootcoordinatedonlylogwrites. Newguard checks currentDB rawstatus/hash/count beforeprepare/enqueue/queuepreflight/attempt andactualPOST; confirmedRetry reusescanonicalmaterial withoutrefetch; onlyscreenshotfields refreshed transactionally, attempt evidence irreversible.
- Review/fixes: independentreview found (a) staleactivekeyexpiry (reactive30s/focusclockfixed), (b) ambientComposevars redirectingdata (pinnedruntime paths/env/port/image androllback), (c) mixedpathfields/case-insensitiveJSON bypass (exactendpoint-specific whitelist+regressions), (d) nativeautomationignoringpendingreview (canonicalmultiboundarygates+atomicrollback/lease tests). Allfourrecheckedresolved. Realbrowserthenreproducedmachine401globallyredirectingadminsessiontwice; debuggerisolatedmachinev1nativefetch beforeJWT/desktop interception; independentreviewand31actualwrapperVMtestsPASS. No broadAxiosrewrite orrealWailsbinaryvalidation.
- Actualchecks: fullWindowsGo1.23 offline suitePASS; focusedproxy/downloader/settings/repair/guard/handler/repository testsPASS; independentfivecanonicalpublicationtestsPASS; proxyLinuxGo1.25network-none suitePASS;12Pythondeployment/smokeguard testsPASS;31NodefetchauthtestsPASS. Fullfrontend50-fileOxlint+ESLintPASS beforefinalmainfix; typecheckPASS. NativeDockerLinux fullGotests/serverbuild andVueproductionbuildPASS beforefinalmain-onlyrebuild; finalUIrebuildnowpassedandcanaryrecreated, finalfullbrowserQAongoing. Allremainingfinalchecksreportedlater, no localappserver.
- Mediaactualvalidation152: amd64ptmove-media-agent:0.3.1 image5d177d787c7b0762716a5134ee392b8b9819551956c86205a510a37a5ec2f2a0,size217841853. Go1.25realtoolintegrationPASS (FFmpeg16ssyntheticvideo/mpvframe/optimization/mockedPixhost/5previewframes), no network. Separateephemeralnonroot/read-only/capdropcontainer passedauth/path/MediaInfo/5previewchecks, thenactualofficialPixhostfinalize1syntheticframe HTTP200 andfullimageBBCodePASS. Tokeninmemory/envonly, notlogged/archived; testcontainerremoved. No actualNAS/qB/trackeror81requests.
- Buildcorrections: firstagentAPT HTTPSmirrorfailedbecausebasehadnoca-certificates; usedsignedHTTPAPTbootstrap(defaultofficialstillHTTP), keptPixhostHTTPS. NativeVuebuildexposedprettierinlineeventsemicolonremoval; extractedtwoDownloaderhandlersandbuildpassed. ARM64isolatedBuildKitinitialdirectDockerHubmetadata timedout; configuredONLYownedbuilderwithserverexistingTencentmirror, rerunARM64imagebuildPASS usingbundledQEMU (nohostbinfmtregistration). PhysicalARMNASruntimeunverified; architectureimageexport/loadchecksongoing. Buildermemory1536m/CPU1/parallelism1, defaultbuilder/unrelatedservicesunchanged.
- Data/backup:152native0.3.0stillhealthy18120atwriting,0.3.1isolated18121only. ConsistentonlineSQLitebackupintegrityok; allnativeautomation/queue/siteprofile/machinekeycounts0. D ZIP GPT-PTMOVE__2026-11-16_000307__数据备份__升级API031前原生数据库.zip SHA2568c131f269f411b2185fdf76c73a96fc0e23658cabaf6d05d4900743e6d649ffc verifiedCRC/databaseSHA/SQLiteintegrity,INDEXappended. DBonly,matchingconfig/env/instancekeysretained152; notcompletebackup. Rootrecordedruntimefilefingerprintsinprivate152metadata forpost-upgradepreservationcheck; no secretsinthislog.
- Git/deploymentstatus: allnewsourceuncommitted/unpushedinisolatedworktree; no0.3.1tag/formalReleaseyet. Lateststagednativepatch55filesSHA2567f2950b16079f1972fec9c7c49ba87b807bd59f6987573b539ae6fbb72539d24,followedbyseparateDownloader/main/.dockerignorefixes; doNOTtreatoldpatchasfinalrelease. Remaining: finalbrowser/targetlint,canaryAPIacceptance,project-onlynativeupgrade+readonlyproductionchecks,archiveamd64/arm64agent+appimagesonD,commit/push/tagandlinkedrecords. GenericLinuxNASpackageisrequested; no actualNASaddress/pathprovided, noNASinstallationclaim. Prior0.3.0formalRelease stillpendingattachments.

## PTM-20261116-026 — 2026-11-16T00:57:32.2754490+08:00 — root

- Purpose/files: final152deploymentverification, README/DEPLOYMENT/checkpointstatus, source/imagepackagerextra-image support andexcludedrebuildableproducts, generatedwebui/dist andproxy/pt-nexus-box-proxy deliverypernexus/AGENTS. UI/runtimecodefrozenaftermainfetchfix. Buildproductsreadfromverifiedimageson152, notexecutedlocally; DtransporttarSHA25624ae028837853596a630bf603cb77847d75eb095bafa4ae0bbbc3534ee073053 andamd64proxybinarySHA256f16eaa8b5cf2f0c3dda39db1691b07d8ddbd77657ee4bf34ca6517943d68c026 verifiedbeforeGitstaging. Dockerarchivescontainbuildproducts; sourcearchiveomitsrebuildabledist/binaryandrecords exclusions.
- Finalvalidation: fullWindowsGo1.23/fullLinuxDockerGo1.23 testsandbuildPASS; Go1.25proxypuretests+actualffmpeg/mpv/syntheticmock integrationPASS;12Python guards+31actualfetchwrapperNodeVMcasesPASS; completefrontend50filesOxlint/ESLintandfinaltargetmain/AI/DownloaderchecksPASS; typecheck/VitePASS (existingbackgroundasset/sharedchunkwarnings). Finalcanarybrowser7PC/mobile screenshotsPASS, includinglocal-onlyNASmode/tokenform interactions withoutsaving, livekeycreate/readprobe/expiry/revoke/clear, sanitizedpackage/OpenAPI, independentmachine401staysloggedin andadmin401redirect; noexternalrequests/unexpectedconsoleerrors, businessstateunchanged. QAinitialNASformlocatorshitElementPlushiddeninputs; changedharnesstovisiblelabels, noapplicationchangeorforcedclick. LatestreportonlydefinesPASS; historicalfailureJSONretainedasdebug evidence.
- FinalcanaryAPI10groupsPASS afteradjustingoldsmokeexactkeymetadatacontractfornewstatusandconnection; earlierre-runhitexistingnondeletableprofilefixture409, soassertedtestprojectlabels/mountthenrecreatedONLYdisposablecanarydata/envandreranPASS. Actualproject-onlynativeupgradePASS; previousJWTvalid, APIrules/tasks/runs/keylistsunchanged, config/env/instancekeyfingerprintsunchanged. ProductionreadonlyAPI6groups+browser4PC/mobile screenshotsPASS, console0/unexpectednetwork0/stateunchanged. Finalserviceimage df6476edac6376a9dcc749e536a484173ce551d3e44d4f114512f10c0d5f650d on152:18120; originalpasswordpreserved, defaultsalloff. Oldnative0.3.0imageandDBbackupretained; Python0.2directory/containerretainedstopped.
- Mediaartifacts: amd64agent5d177d787c7b0762716a5134ee392b8b9819551956c86205a510a37a5ec2f2a0 export215225226bytes SHA256b23c667b6f11ef808b1c45b92f536a4a28d273a0112107c57d76bf431d76a5fb; arm64agent4b7e02a1a365b40781f484dca1d8c2a4f52c9944245016733bd45306036059ce export198653220bytes SHA2562971047a82260bf0ae6ecf93f611887158cb54b137ad7f9a4c95ab9e60612c4b. Nativeappgzip368924702bytes SHA2567c7fa54b769f400cd468557b749ab4566805ca4672d769ea995fe9a893004f6a. ALL3dockerloadverifiedon152; ARMphysicalhardware/NASnotverified. Ownedcrossbuilderstopped, nohostbinfmtchanges. ActualPixhost1syntheticimageHTTP200, no actualuserfile/NAS/qB/tracker/81request.
- Archiveprogress: bulkSCPthenSSHstreamcopieswereinterruptedafterlackofvisibleprogress; actualpartial193789952/249888768bytes showtransferdidoccur, soinitialzero-lengthopenfileobservationdoesnotestablishanetworkstall. Usingtemporaryproject-onlyTLSartifactcontainer18123withpinnedselfsignedcert, ephemeralheaderTokeninmemory/envonly, exact3fileallowlist/read-onlymount tofinishDarchive; noSSHprivatekeycopied. ContainerandTLSprivatefileswillberemovedaftertransfer. Noarchivecompletionclaimyet; fullDhash/ZIP verificationandINDEXrecordpending. No secretsinGit/logs/package.
- Git/status: changesreviewedandvalidated, authorizedfeaturecommit/merge/push/tagv0.3.1next; formalGiteeReleaseattachmentsnotpublished. NeedlinkedrecordafterGit/archive/cleanup. Softwareonlineandhealthy, no localappserver; genericNASDockerpackageawaitsuser-specificendpoint/pathforinstallationunderapplicable81doubleconfirmationrules.

## PTM-20261116-027 — 2026-11-16T01:24:53.146662+08:00 — root/release031_parts, independently checked by api_management_review

- Purpose/files: docs/DEPLOYMENT.json, PTNEXUS_CHECKPOINT.md, REMOTE_SCREENSHOTS.md, RELEASE-v0.3.1.md and proxy/README.nas.md record actual0.3.1delivery and NASinstall/ARMretag instructions; package_nexus.py derives restore tags from VERSION rather than hardcoding0.3.1; .dockerignore excludes temporarytransfer directory. Source/runtimefeature remains frozen; no new build/restart needed for metadata/packaging changes. Root coordinates sharedlog/INDEX.
- Git: feature squash-integrated as ed0bcc6a39223993368d3489db7172bb1678bf72, master pushed; annotatedv0.3.1 tag object a41254ec53f43f6e70e32f96381e8dd292f948a2 peels to samecommit. gitls-remote recheckedPASS. This metadata worktree261116-api-release-record-b102 is not yet committed/pushed at writing; separatetopic avoids folding or moving feature/tag. Generateddist/proxybinary included in featurecommit per nestedAGENTS.
- Archives: all3Docker archives transferred toD staging/release031 and rootrehashed against152; independent reviewer also verified exactsizes/SHA, fullgzipCRC/tar, matchinglinuxamd64/arm64tags and all21OCIblobdigestpaths. Historicalrelease GPT-PTMOVE__2026-11-16_011943__历史发布__Nexus-v0.3.1.zip,793113781bytes,SHA256cd9e93dd58d0988e1a91f1092ecee7bc034c8d4e4853858acd22655184fe5c1f includes source+3images; rootZIPCRC/all741manifestSHA passed. Earlier01:18:41archive918938a523c259605496ba2ca0354fb5bdee6e4bac9635d721bf7d1a1a335252 retained; latest01:19:43adds dynamicpackagerversionfix. Linkedmetadata/notes written after archive are not retroactively inside it. No livecredentials/data/keys/media; notfullruntimebackup. INDEX records preserved/appended.
- NASinstaller: D source-only GPT-PTMOVE__2026-11-16_010801__源码快照__NAS代理安装包-v0.3.1.zip SHA256d334d6a9caa8ffb385ee1ba9b7ec99e87dc96d3975224c8a7d250235e7c3f3b2; CRC/all23manifestSHA independentlyPASS. SourceDockerbuildpackage has no prebuiltimages; matchingimages included in fullrelease. Gitee80MiBrawchunk/ZIPwrappers currently generating; finalreassemblychecks pending at writing, separately linkedbelow.
- Digest correction: amd64agent5d177d... previously recorded in025/026 is original BuildKit OCI image.index ID. Loadedtag/savedplatformmanifest is90039d0938cab9ec0116e4cf34fc22480711ccf9a6406d06c2348c9471cb3225. Project-only152dockerinspect proves both have identical3RootFSlayers; publicmetadata now distinguishes build_index_id and currentimage_id. Exportbytes/SHA unchanged; no media/runtimechange. Appdf6476... andARM4b7e02... already matchplatformmanifests.
- Deployment/cleanup:152service healthy0.3.1on18120 rechecked; canary/mediaQAcontainers removed and ownBuildKit stopped previously026. TemporaryTLS18123helper removed, exactprivate transfer031directory removed, interrupted .downloadfragments removed after3fullhashchecks; finalpath/containernamerecheckedPASS. No unrelatedservice/dataoperation; no realNAS/qB/tracker/81access, no localappserver. PhysicalARM and actualNASinstall remain unverified.
- Publication blocker: browserGiteeRelease/new redirected to login with expiredsession. User handoff/request to sign in remains pending; no credentials filled/read, attachments selected/uploaded orCreate submitted. No alternate authentication/browser route used. FormalRelease release_published=false, code/tagpublication and runtimeavailability are distinct. Releaseinstructions/checkpoint concrete and ready for login.
- Checks: priorfullGo/UI/API/mediavalidation026 unchanged; currentJSON parse, Pythoncompile, diffcheck passed. Root visuallyinspected latestNASmobile form; synthetic credentials masked. Independentmetadatareview found/fixed hardcodedfutureversionrestoretag and confirmed currentARMtags/sourcepackage. Remaining: finishpartsreassembly, finalmetadata commit/push/remote publicrecord sync and resume checkpoint; formalGiteeattachments awaituserlogin; actualNAS install needs endpoint/path and freshapplicable81doubleconfirmation.

## PTM-20261116-028 — 2026-11-16T01:31:31.563054+08:00 — root/release031_parts

- Purpose/files: linked027deliverycompletion; docs/DEPLOYMENT.json and PTNEXUS_CHECKPOINT.md point to finalrecoverableuploadwrappers, REMOTE_SCREENSHOTS splitsamd64/arm64loadingexamples; D archiveINDEXdistribution record appended afterrereadingtail. No runtime/sourcefeaturechange.
- Artifacts/checks: finalD staging/gitee-v0.3.1-parts-final contains10ZIP_STOREDwrappers,9rawchunks83886080bytes andlast38139061; largestwrapper83892407bytes,allbelow100MiB. Workeractuallyextracted/reassembled793113781byteZIP,SHA256cd9e93dd58d0988e1a91f1092ecee7bc034c8d4e4853858acd22655184fe5c1f,CRC/all741manifesthashesPASS. Rootreadreport/code,checkedall10actualsizes/compression/embeddedjoiner andfirst+lastwrapperSHA/CRC independentlyPASS. Earlierparts directory and allarchives retained, usefinalonly.
- Restorefix/partialattempts: firstworkerwrapperverifier mishandledduplicatecommonfiles; harnessfixed andonlyownpartialverificationoutputsremoved. Rootthenfoundjoinerunconditionallydeletedpreexisting .joining onexclusiveopenfailure; created_by_this_run cleanup nowpreservesforeignfiles,atomic os.link refuses concurrenttargetoverwrite, failedpublicationretainsverifiedtemporarywithclearpath. Bothpreexistingtarget/temp regressionfixturesPASS. Finalharnessinitiallyinvokedwrongfixturejoinerpath; correctedthenfullregressions/reassemblyPASS. These packagingattemptsdidnotmodifyoriginalrelease/history orruntime.
- Deployment/records: publicdeployment.json syncedto152 andJSON/featurecommit/release_published=false verified. InitialSSHinlinevalidation failedPythonSyntaxErrorbecausePowerShellquoting, beforeanyremotevalidationaction; reranviaPythonstdinPASS. Production0.3.1healthyfrom027; no rebuild/restartperformedforrecords. No81/NAS/qB/trackeroperations.
- Git/publication: featureandv0.3.1alreadyremoteverified027. This linkedmetadatachange willbecommitted/reviewed/merged/pushed separately understandinguserauthorization; release tag remainsunchanged. CurrentformalGiteeRelease stillunsubmitted/unuploadedbecauseuserloginhandoffpending; checkpoint/notes/partsconcrete. Afterpushignoredresume recordsfinalmasterhash. CurrentmetadataJSONparse/Pythoncompile/diffcheckPASS beforecommit; no redundantfulltestrepeatfordocs-onlychanges. RealNASinstallation/physicalARM/actualdownloadandpublishremainunverified.

## PTM-20261116-029 — 2026-11-16T18:49:01.094022+08:00 — root, preflight by api_management_review

- Purpose/files: README.md, docs/DEPLOYMENT.json, PTNEXUS_CHECKPOINT.md, PTNEXUS_FORK.md and RELEASE-v0.3.1.md now record successful formal Gitee publication instead of the expired-login checkpoint. User explicitly completed the browser login handoff; root reused the same selected IAB2/tab1. No credentials read/filled, no permission/access changes. Only documentation/metadata changed; runtime source/images remain frozen.
- Publication: submitted Create once after selecting existing non-release tag v0.3.1 and filling finalnotes. FinalURL https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.1 visibly shows latest stable release, title PTMove Nexus v0.3.1 · API管理与NAS补截图, pagepublishedtime2026-11-16T18:45+08:00 (minuteprecision), correctcommit ed0bcc6a39223993368d3489db7172bb1678bf72 and ALL11 customdownloadlinks. No previewmark. TenfinalDockerbundleparts plus NASsourceinstaller attached; sourceautoZIP/TAR are additionalgeneratedlinks, excludedfrom11count. No repeatedCreate/re-upload/newtag.
- Artifactverification: independentpreflight checked all11actualfilenames/lengths,793227405bytestotal(<decimal1GB),eachwrapper<100MiB, finalarchive/reassemblymanifestagreement. Prior028actualreassembly/CRC/all741SHAremainvalid; no redundantlargerehash. Browser finalDOM filenamesmatch allexpected11andstable release/download paths. Verification scope is visiblefinaldownloadlinks, not re-downloadedremote-bytehashes. FullJPEGproofandpublication-evidence.jsonsavedunder Darchive2026-11/staging/gitee-v0.3.1-publication; rootviewedproof, DINDEXpublishedrecord linked028distribution appendedafterrereadingtail. No credentials/liveDB/media in softwareattachments.
- UIpartialattempts: firstclick on hiddenfileinputtimedout withoutchooser/upload; documentedvisibleupload-areachooser worked. Create starts asynchronous upload; waitedforprogress, no blindretry. Aftercompletion progresslocator timedoutbecausepage hadnavigated; readfreshfinalpageandverifiedsuccess, no repeatedsubmission. Browser CUAcompletionrecordedafterfinalproof/markDeliverable. Originaluserbrowserlogin preserved.
- Git/deployment: prior deliverymetadata2417088b680770afe93c5c9af633bb084add380e isalreadyonmaster/Gitee; releasefeaturetagremainsed0bcc6. This newseparate release-publicationworktreeb103 record isnotyetcommitted/pushedatwriting; authorizedreview/merge/pushnext, withoutfoldingorretagging. CurrentdocsJSONandgitdiffcheckPASS. Public152deploymentmetadata sync/ownedservicehealthchecknext; no rebuild/restart orapplicationconfigurationchangeplanned. No81/NAS/qB/trackeraccess orlocalappserver.
- Remaininglimits: realNASinstallation, physicalARMmediaexecution andactualtrackerdownload/publishremainunverified; automaticmodule/schedule/publishdefaultoff. ActualNASendpoint/path/transportstillneeded, and81operations requirefreshapplicabletwoseparateconfirmations. Prior0.3.0formalpublicationisahistoricaluncompletedstep; current0.3.1isfullypublished, donotrerunoldinitialcutoverorreleases.

## PTM-20261116-030 — 2026-11-16T18:49:46.965868+08:00 — root

- Purpose/files: linked029 actual152publicationmetadata synchronization and delivery verification; rootlog and Dpublication-evidence.json updated, no application/runtime configuration orimagechanges.
- Actualchecks: SCP docs/DEPLOYMENT.json to project-owned /opt/gpt-ptmove-nexus/deployment.json succeeded; remote JSON release_published=true/count11 and exactfileSHA match localPASS. Named productioncontainer healthy and unchangedimage df6476edac6376a9dcc749e536a484173ce551d3e44d4f114512f10c0d5f650d; loopbackhealth service ptmove-nexus/version0.3.1PASS. No restart/build/cleanup orotherprojectoperation. No NAS/qB/tracker/81access.
- Git/publication: actualGiteeRelease and11links alreadyverified029; upcomingdocumentationcommit/review/merge/push records029/030 withoutmovingv0.3.1. PriorfunctionalGo/frontend/API/media tests unchanged, no runtimecodechangejustifiesrepeatingthem. LatestmetadataJSONconsistencyPASS; diffcheck andindependentmetadatareviewfinalresultsnext. Userloginhandoffclosed; remainingrealNAS/physicalARM/actualtrackerintegrationlimits unchanged.
- Final independent review: publication metadata and all11 attachmentnames/tag/timestamps agree with saved page evidence; JPEG SHA and visual latestbadge/commit/links recheckedPASS, active pending-login text removed while historical records retained. JSON assertions/diffcheckPASS; no network/browser/server access by reviewer.

## PTM-20261117-031 — 2026-11-17T23:32:59.071058+08:00 — root / screenshot_doc_facts

- 目的与文件：新增 docs/SCREENSHOT_REPLACEMENT_DESIGN.html，按用户要求把截图替换思路整理为中文单文件 HTML；docs/REMOTE_SCREENSHOTS.md 增加入口。包含三端数据流、六步处理、替换/失败规则、保留有效旧图示例、NAS 配置、临时文件与 Pixhost 生命周期、实际验证范围及源码索引。自带 CSS、打印样式和 CSP，无脚本、外部字体或自动加载资源。HTML SHA256：8ac290828cd7b0b15443575f54955d2d548fae56a2d565347e5b4c81728578c1。
- 内容复核：独立只读代码核对及全文事实复核通过。明确抓取阶段 HTTP 可用性校验与发布阶段语法/状态校验的区别；正式补图目标5、预览默认12、合并去重后默认门槛3、人工确认至少1；部分成功和未知字幕仍可能待确认，确认后另行重试，不把确认描述成自动发种。去重为 URL 精确字符串；实际 NAS/物理 ARM/真实发种未验证边界保留。
- 实际检查：使用已有 Playwright SDK 与已安装 Edge，以 file:// 渲染 HTML，不启动本地应用服务器。1280×960 与390×844检查通过：7章节、唯一主标题、无重复ID/失效页内链接/横向或元素溢出，目录跳转和源码详情展开正常；0外部网络请求、0控制台错误、0脚本和外部资源，四组正文配色对比度均≥6.22，打印时隐藏导航已核对。6张截图及 qa-report.json 保存在 D归档根目录2026-11/staging/screenshot-design-20261117；root实际查看桌面概览与手机架构图，无排版问题。未做物理打印/PDF逐页分页验收。
- 工具适配：普通模块解析最初找不到 playwright，npx/共享视觉脚本不在当前路径；改用已有运行时中的 Playwright SDK 和 msedge 渠道完成验证，没有安装依赖或访问互联网。未运行 Go/应用测试，因本次仅文档变化。
- Git/部署：独立 worktree261117-screenshot-design-b104，当前尚未提交/推送，按已有项目提交授权完成文档提交、审查、集成和推送；不移动 v0.3.1。没有152部署/重启、站点/NAS/qB/81访问；软件版本和镜像不变。本任务交付离线HTML，不新建发行版。

## PTM-20261118-032 — 2026-11-18T17:59:10+08:00 — Codex root，飞牛远程补图实测与 Pixhost 计数修复

- 部署。飞牛刷流 amd64 加载已核对 SHA256 的 v0.3.1 媒体镜像，使用与 qB 相同的普通 UID/GID，只读挂载 `/keep`、`/temp`。Compose 位于飞牛 `/opt/ptmove-media-agent`，媒体端口只监听回环。152 的 `ptmove-feiniu-link.service` 在本项目容器网络内提供加密连接，飞牛专用密钥只能调用 qB 或媒体端口；旧的连接实现遇到空闲会话耗尽，已加 EOF/超时处理和独立连接池。容器重建后连接自动恢复，双端服务已验证。目标为独立飞牛的已指定中转入口，未操作 81 主机或中转配置。
- 样本与结果。qB v5.3.0 的“重复”标签有 29 项，29 项下载完成。第一段视频命中原有分集限制，原生预检跳过修复。第二个样本为电影，使用真实视频、远程截帧及真实 Pixhost 服务，在 152 用模拟来源资料运行生产后台修复及 SQLite 入库流程。正式查询 Handler 从隔离库读回 5 张 PNG，字幕未识别为可用，审核状态 pending，发布队列及发布日志均为空。首次整图读取遇 35 秒超时，随后复用这五个地址，全部 HTTP 200、FFmpeg 完整解码为 1920×1080、内容 SHA256 均不同。本轮没有获取新的种子、修改 qB 的标签或媒体权限、向真实站点发种，生产库未添加模拟草稿。物理 ARM 仍未实测。
- 修复。在原生入库测试中，原程序把 5 张图记作 10 个图片地址。抓取与修复改为共用 `nexus/server/internal/service/acquire/extract/image_urls.go`。调整同目录 `shared_helpers.go` 和 `processing/repair/images.go`，回归用例在 `processing/repair/images_test.go`。解析器保留 BBCode 的图片正文、查询参数和顺序，去除包裹图片的展示页、同原图的旧 Pixhost 展示链接、Fragment 与 Pixhost CDN/缓存别名引起的重复计数。模拟来源的空白测试海报在首次电影测试时也被原生流程转存到 Pixhost。实测修正前后的两轮电影截图各上传五张，网络读取复核没有重新截图或上传。
- 验证。在 152 离线 Docker 环境执行 gofmt、全量 `go test -trimpath -p 1 ./...` 和后端构建通过。最终镜像 `ptmove-nexus:0.3.1-pixhost.1`，ID `sha256:c8da38daa2c0ebf0e25d8d9a5d922fb2d7f022535f1bcdb127b9105e9d9040af`。隔离启动和健康检查通过，随后切换 152 生产容器，数据卷复用，旧镜像可回退。在正式管理 API 再次验证已保存下载器的 qB 和媒体认证，并使用 qB 实际保存目录及种子名返回 5 张预览。自动化、月度计划、自动发布均关闭，每月限额 6。临时 qB 凭据已经移除，密钥及真实媒体未纳入 Git。未启动本地应用。
- 文档与交付。更新 `docs/DEPLOYMENT.json`、`docs/REMOTE_SCREENSHOTS.md` 和 `docs/SCREENSHOT_REPLACEMENT_DESIGN.html`，核对 HTML 锚点和无脚本约束。四个 Go 源码/测试文件已同步至 152 的项目构建目录，部署 JSON 同步后 SHA256 一致。152 提供服务于 http://152.136.236.238:18120/ 。Gitee 上原有 v0.3.1 发行附件对应原始镜像。记录时修复分支尚未提交/推送，按项目已有交付授权随后提交并核对远端 master，本次没有新建发行标签。


## PTM-20261118-033 — 2026-11-18T21:17:47+08:00 — Codex root，飞牛通过 HTTPS 主动领取任务

- 实现。新增 `nexus/proxy/pull/` 的共享 Docker 镜像、Hub、飞牛 Agent、双端 Compose 和测试。152 在独立的 PTMove Docker 网络用双端鉴权的内部接口提交请求，公网仅发布 HTTPS 28443 的两个领取和回报端点。飞牛以独立 qB 和媒体凭据访问本机 8080 与 19090，qB 查询与媒体操作各自排队。协议限制请求副作用、目的地址、体积和方法，结果用任务 ID 与租约匹配。同一结果可重传，已领取的任务不由 Hub 自动重放。连接配置错误命中 81 地址时在实际拨号前拒绝。
- 主程序保护。调整 `nexus/server/internal/service/processing/repair/screenshot.go`、`screenshot_preview.go`，为带凭据的仅媒体代理保留远端错误。远端结果不明确时报错，400 路径候选仍可继续，避免开始本地正式截图和再次上传。新增 `remote_screenshot_no_fallback_test.go`。当前 Hub 只提供 qB 查询，种子导出、空间检查及下载写操作需单独配置。Hub 队列和回执保留在内存中，重启使进行中的请求失败。正在截图时媒体连接检查可能返回忙，qB 查询独立运行。
- 部署。152 的主程序换为 `ptmove-nexus:0.3.1-pull.1`，镜像 ID `sha256:cd023e8073aae7407bf468121425fc388e69b168a44b897b29e36c2df86044f5`，复用原数据目录和 18120，应用健康。Hub `ptmove-pull:0.3.1-pull.1` 位于 `/opt/gpt-ptmove-nexus/pull-agent`。飞牛在 `/opt/ptmove-pull-agent` 启动同镜像的 Agent，沿用原媒体容器和只读媒体挂载。按用户澄清，SSH 只用于飞牛 Docker 安装及核对状态。验证了实际目标为独立飞牛，没有操作 81 主机或中转配置。飞牛代理保持两条直达 152:28443 的连接，152 上旧 `ptmove-feiniu-link.service` 停止且禁用开机启动。PTMove 容器自身只监听应用和 Docker DNS，旧回环业务端口不存在。152 现用下载器配置只含本项目的 Controller Token，qB Token 在飞牛本机使用。
- 验收。152 上通过 Pull 全量 Docker Go 测试、go vet 和主程序全量 Go 测试，临时空库镜像启动通过。公网 TLS 校验 IP SAN 成功，未认证领取返回 401。新路径真实读到 qB “重复”标签下 29 个完成任务。选取《皇冠高地》真实视频，停掉旧 SSH 转发后，生产管理接口返回 5 张预览，原生后台修复用模拟来源资料从 0 张补齐 5 张，在隔离 SQLite 入库并通过标准草稿查询读回。字幕确认状态为 confirmed_chinese，草稿状态 none，发布队列和日志都是 0。Pixhost 返回五张 1920×800 PNG，152 下载、FFmpeg 完整解码和不同 SHA256 校验成功。验收尺寸最初套用了上一部电影的 1920×1080，读取 PNG 头后修正并复用原链接，没有再次上传。SFTP 首次写入空文件错误已用明确读写模式恢复。
- 交付。更新 `docs/REMOTE_SCREENSHOTS.md`、`docs/SCREENSHOT_REPLACEMENT_DESIGN.html`、`docs/DEPLOYMENT.json`。三个主程序守卫源文件和部署元数据已同步至 152，部署 JSON 哈希一致。未在本机启动生产服务。自动化、月度计划和自动发种均未启用，真实站点发种和物理 ARM 硬件不在本次实测范围。记录时改动待提交及推送到 Gitee；原 v0.3.1 的发行附件保留对应历史镜像。本次运行 URL 为 http://152.136.236.238:18120/ 。


## PTM-20261119-034 — 2026-11-19T14:35:52+08:00 — Codex root，81 PTNexus 数据复制到 152

- 范围。已按 81 服务器保护规则分别取得两轮明确确认。81 只读定位正式 `pt-nexus` 容器的 `/app/data`，使用 SQLite 内存备份生成一致性快照。81 上未写文件、停机或改配置。初次流式打包遇 Python 3.11 tarfile 参数差异，改为显式 gzip 流后通过。源库及配置、两份站点/扫描缓存只传到 152，位于 `/opt/gpt-ptmove-nexus/imports/ptnexus81-20261119-x1gya610`，目录和文件限制为 root 访问，SHA256 和源副本完整性通过。
- 导入。用户明确无需备份 152 原有数据。在线事务先模拟并回滚，再实际合并 152 的 `data/pt_stats.db`。导入 69 条转种资料、135 条旧种子历史、467 条上传统计历史。旧飞牛下载器域名和现有 329 个可见种子哈希均匹配，转到当前飞牛下载器 ID，保留已有实时记录。旧种子历史按目标程序的隐藏规则导入，以免被当成当前在库资源。目标总计 464 条种子记录、796 条上传统计。95 个同网址站点补齐空白账户字段，1 个网址不一致的站点账户以及旧版独有的批量优化记录和 ID 迁移表保留于 152 上的源快照。未导入运行中任务，目标账号、飞牛 HTTPS 领单配置及应用镜像未改。日志中不包含站点凭据。
- 验证。源库与目标 SQLite 的 integrity_check 均通过。69 条资料键全部匹配，抽查 3 条经正式 PTNexus 草稿接口读回。旧资料中 22 条需截图审核，发布队列、发布日志、自动化运行与任务均为 0。自动化、定时和自动发布仍关闭，未执行转种操作。未重启容器、未备份 152 原有数据、未修改应用代码。项目本地仅追加本记录，本轮未提交或推送 Git。


## PTM-20261119-035 — 2026-11-19T18:35:39+08:00 — Codex root，发布核验与 152 登录账户更新

- 权限。用户明确终止 81 上次复制任务的操作授权。本轮全部读取、账户更新及验证均未连接 81。NovaHD 和 Pixhost 验证仅经 152 对已经确认的 HTTPS 域名发送 GET，未重发或编辑站点记录。
- 发布。152 的手动发布队列第 1 条在 18:28:57 完成，源为织梦、目标为 NovaHD。外层队列结果为 success，站点发布记录为 exists，对应详情 ID 28365。结构化结果显示目标已存在且未执行自动编辑。自动添加到飞牛 qB 请求失败，HTTP 403。核对详情页返回 HTTP 200，正文中的五张 Pixhost 地址全部与任务中准备的五张匹配。逐张 GET Range 返回 HTTP 206，PNG 头均为 3840×2160。
- 账户。通过 152 PTNexus 的原生认证 API 更新用户名与密码，已用新凭据验证登录及受保护设置接口，bcrypt 哈希持久化，非认证配置经比较保持原样。只修改 152 的应用认证配置，密码未写入记录。应用未重启，未改应用代码。项目本地仅追加本条，本轮未提交或推送 Git。


## PTM-20261119-036 — 2026-11-19T20:27:03+08:00 — Codex root，修复飞牛 HTTPS 领单的发种后添加 403

- 原因。152 原 Hub 对 POST /api/v2/torrents/add 返回 HTTP 403 和白名单拒绝提示，飞牛 qB 尚未收到请求。发种自动添加在受支持站点取到种子字节后，由 PTNexus 原生客户端提交 multipart 文件。修复包括 `nexus/proxy/pull/protocol.go`、`qb_add.go`、`hub.go`、`agent.go`、对应回归测试和两端 Compose。放行固定路径的一个 .torrent 文件，总请求最多 4 MiB，其他 API 保持原限额和权限。URL 表单、磁力链接、种子导出、删除、重检及改 Tracker 不在该权限内，站点下载器只拿到直链而未成功读取文件时仍无法走本接口。继续使用 HTTPS 领单、原租约及结果回传规则。
- 兼容。首轮临时无 Tracker 种子成功添加，但飞牛 qB v5.3.0 未按旧 paused 参数停止，已精确清理该测试项。最终 Agent 校验请求后自动补发等值 stopped 字段，保留原文件内容和 paused 以兼容旧版本。测试覆盖最大请求边界、非原生表单拒绝、回执重传与本机 qB 操作只执行一次。
- 验证。152 上隔离 Docker 执行 gofmt、Go 全量 Pull 测试和 go vet 通过。最终实际使用 PTNexus 的 AddTorrentDataWithOptions 对 98,464 字节、无 Tracker 的临时种子经 152 和飞牛 HTTPS 领单完成添加。按准确 info hash 读回正确目录与 stoppedDL 状态。飞牛本机仅清理该临时测试条目，未删文件，PTNexus 两张种子缓存表均无测试残留。上线后 qB 查询 200、截图接口探针 405 符合原协议，URL 添加和删除 403。发布队列和发布日志最后一次更新均在本轮之前，未重新发种。亲验状态时初始断言误以为只有第 1 条历史发布记录，已根据实际 7 条历史记录的时间重新核验。
- 部署。152 的 Hub 与飞牛 Agent 均升级 `ptmove-pull:0.3.1-pull.3`。Hub 镜像 ID `sha256:d5387e28bc00fc660f9679dd5a08f1f069e758dc332696e490e35e13b6d438c6`，飞牛 amd64 config ID `sha256:fb86f7ac3f52af9cf7a9f52404d76da2dec6777640a478b684efcaefa332cc27`。152 PTNexus 原容器健康，服务地址 http://152.136.236.238:18120/ 。旧转发仍停用。飞牛安装仅使用其独立管理入口，运行链路经 152 HTTPS 28443，未操作 81 主机或中转配置。更新 `docs/DEPLOYMENT.json`、`docs/REMOTE_SCREENSHOTS.md`，部署 JSON 已同步至 152 并比对 SHA256。工作区保留前两条未提交记录，本轮未提交或推送 Git。

## PTM-20261119-037 — 2026-11-19T20:59:00+08:00 — Codex root，STARPT 界面、任务状态、源站禁转保护和插件

- 源站限制。修改 `nexus/server/internal/service/{acquire,migrationflow,publish}` 及 HTTP 路由。统一直接发布、批次和队列的可信源站判定。在上传前读取同源详情，拒绝禁转或权限未知的资源，并绑定源站种子标识和实际发布字节。客户端不能通过改资料或提交允许字段覆盖。历史队列缺少可信种子标识时会停止。已存在记录的后续自动编辑和自动添加同样受入口限制。
- 资料与任务。修改 `crossseed/query.go`、`repository/publish_log_repository.go`、`migrationflow/task_monitor.go`。一键多种投影海报、截图、简介正文、MediaInfo 缺项并逐条展示。转种记录按站点显示发布成功、已存在、失败、未完成等，自动添加显示独立结果。前端提供带目标站和资料摘要的批量确认，按队列分组追踪，状态不明确时不自动重发。
- 界面与插件。修改 `nexus/webui/src` 的导航、登录、首页、设置、任务记录及转种相关界面和响应式布局，加入 STARPT 标识、线框图标、适度半透明导航和减少动态效果支持。移除 Wiki 入口，源码与反馈入口指向项目 Gitee，站点导入和机器 API 保留。新增 `server/internal/service/plugins`、`plugins_handler.go`、`PluginsView.vue` 和 `docs/STARPT_PLUGINS.md`。管理员可以在数据目录放入或移除 JSON 后在 Web 刷新。清单可展示文字、现有页面链接和只读指标，扩展后台能力需增加服务端实现。
- 验证。152 上 Gofmt、无网络 Docker 全量 Go 测试和编译、Vue 全量类型检查和生产构建通过。状态与脱敏 Node 测试六项通过。隔离应用验证 JWT 边界、插件加载和移除、无效文件诊断、未知源预检、资料缺项、未完成筛选、站点结果与自动添加独立。Edge Playwright 桌面和手机验证登录、导航、插件、任务记录、种子维护，通过后清理隔离容器、测试数据和临时凭据。首次隔离网络不映射宿主端口，改用容器私网地址及 152 SSH 隧道复测通过。真实站点发布及 81 操作次数均为零。
- 部署及 Git。152 的 `ptmove-nexus-ptmove-nexus-1` 已升级 `ptmove-nexus:0.3.1-starpt.1`，镜像 ID `sha256:d902217b5e7cccfe6dd3762f38df7ca5f5f483ed7656f3a9e5ab665730536829`。公网首页和健康接口返回 HTTP 200，生产数据沿用现有挂载。飞牛 HTTPS 领单组件未变动。生产 `/opt/gpt-ptmove-nexus/data/plugins` 已建，部署清单与本地 `docs/DEPLOYMENT.json` 校验一致。自动调度保持关闭。本轮未提交、推送或追加 Gitee 发行版。


## PTM-20261119-038 — 2026-11-19T21:14:38+08:00 — Codex root，准备 STARPT 双平台发行

- 交付范围。提交已在 152 验证的 61 个 STARPT 源码文件和对应部署记录。此前的飞牛 403 修复、实际站点记录和其他本地历史继续保留在工作区。本次标签为 v0.3.1-starpt.1，与发布镜像的构建号一致。
- 检查。逐文件比对 152 构建使用的 STARPT 工作树和发布源码。确认运行镜像 `ptmove-nexus:0.3.1-starpt.1` 为 linux/amd64，镜像 ID 为 `sha256:d902217b5e7cccfe6dd3762f38df7ca5f5f483ed7656f3a9e5ab665730536829`，152 容器健康。扫描待提交文件和已有 Git 历史的常见密钥、凭据和运行时文件，无阻塞项。
- Git 及部署。准备提交、打标签并发布到 Gitee 与 GitHub 私有仓库，发行及附件状态在验证后追加。生产应用继续运行已验证的同一镜像。本轮无实际发种、无 81 操作。


## PTM-20261119-039 — 2026-11-19T21:33:36+08:00 — Codex root，核对 STARPT 发行和镜像附件

- 版本与 Git。标签 v0.3.1-starpt.1 指向 2547a69dc599a0505a074cb8276726a87845370d，Gitee master 和远端标签已核对。Gitee 私有仓库已发布 [v0.3.1-starpt.1](https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.1-starpt.1)。54,310,451 字节增量附件已在发行页确认，SHA256 7a7b8c9ba35a1950a3c4f1311ac0dc1a8f88c9202e01e04465fa3af5334f87f6。用已发布 v0.3.1 基础镜像重组出相同 OCI 镜像标识。 GitHub CLI 仍需设备授权，私有仓库、推送与 GitHub 发行待完成。
- 镜像。导出 152 上健康运行的 linux/amd64 应用镜像。完整 Docker 归档 423,231,489 字节，SHA256 ae74d061ffa1b9567eb1a74e1d536597a271e35cee0574269d9791c51186954e。服务器与本地哈希一致，已检查归档元数据与镜像层，归档不包含容器数据卷。
- 历史归档。已在 D 盘归档带标签源码及 Docker 镜像，校验 ZIP CRC 与两个载荷 SHA256，完整 ZIP SHA256 eda80ba69dea00dcfb0ee8393265855ae4de85233727def8b3c7ad0993545891。项目索引与总索引已追加。Go 测试、Vue 构建及浏览器验收沿用部署记录，本轮增跑六项状态辅助函数回归。
- 部署。152 继续使用原已验证镜像，本次只导出和发布版本。实际发种次数为零，81 操作次数为零。预先存在的飞牛修复、站点测试记录和部署资料保留本地。


## PTM-20261119-040 — 2026-11-19T22:42:12+08:00 — Codex root，发布 STARPT 到 GitHub 私有仓库

- GitHub。已创建并核对私有仓库 https://github.com/Chenyi-1999/gpt-ptmove。master 指向 d1258a23e62ac843bff7b90258ab3c5dd777dac6，标签 v0.3.1-starpt.1 指向 2547a69dc599a0505a074cb8276726a87845370d，与 Gitee 标签一致。正式发行 https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v0.3.1-starpt.1 已核对。
- 镜像。已上传 152 导出的完整 linux/amd64 Docker 归档及 SHA-256 校验文件。GitHub API 返回附件状态 uploaded、长度 423,231,489 字节、SHA-256 ae74d061ffa1b9567eb1a74e1d536597a271e35cee0574269d9791c51186954e，与本机及 152 归档一致。
- 验证和范围。通过 Git Credential Manager 的授权确认账号。直接推送遇到 HTTP 408 和连接重置，经 152 临时 SSH 转发完成 GitHub HTTPS 推送和上传。凭据未写入项目或 152。无需改动生产容器，本轮实际发种和 81 操作均为零。既有本地未提交修改保留。
- 记录。CHANGELOG.md 和 docs/DEPLOYMENT.json 已记录本次结果。此条为本地发行记录，发布标签未移动。


## PTM-20261120-041 — 2026-11-20T00:21:55+08:00 — Codex root，修复种子维护「重新拉取数据」404

- 原因。152 连续三次记录到 POST /api/migrate/refetch_maintenance_seed 返回 404。维护页面从旧版引用该路径，Go 路由一直缺少对应接口。
- 修复文件。nexus/server/internal/bootstrap、http/handler/migrate、service 和 repository 和 nexus/webui/src/views/CrossSeedDataView.vue。先核验原记录身份，才同源拉取。跨站需核验来源限制和内容一致性，更新按原记录的键及旧值执行。缺项保留，失败或竞争保留旧资料。页面忽略已关闭弹窗的迟到结果，成功后重载原记录编辑器。
- 验证。152 无外部网络的 Docker Go 全量测试、Vue 全量类型检查与生产构建通过。隔离容器的健康 200、无权限 401 和登录后的空请求 400 验证通过。源站同源鉴权、转载限制、内容比对、保存范围和 SQLite 并发回归通过。首轮 Go 构建移除多余 import 后通过，随后 152 SSH 短时断开，重连后完成镜像和隔离验证。
- 部署与 Git。152 已替换工作台镜像 ptmove-nexus:0.3.1-starpt.2，ID sha256:cfc6c508c87995ca68b446dc87844fe237b9a9154b8477a25e83c1cd9e0ae857。上线前发布队列为空、自动化停用，上线后首页及健康接口确认。已发布版本的标签和附件沿用原记录。实际站点重抓未代为测试，实际发种和 81 操作均为零。本轮改动已保留在工作区，未提交或推送。

## CODEX-FOLDER-MIGRATE-20261120-phase2-final-GPT-PTMOVE

- 时间 2026-11-20T11:40:42+08:00（Asia/Shanghai），执行者 Codex /root。
- 整目录迁移 C:\Users\50578\Desktop\GPT-PTMOVE → C:\A-GPTProject\GPT-PTMOVE。目录标识与迁移前一致，保留工作区内容。
- 迁后核对目录、各 Git 仓库 HEAD、分支、索引、状态和配置。覆盖 5 个 Git 工作区，修复 3 个 worktree 路径。
- 本机 Codex 入口检查见迁移管理记录。应用级验收未执行。未提交、推送或部署。

## PTM-20261121-043 — 2026-11-21T12:48:33+08:00 — Codex root，种子列表标签显示及截图失败排查

- 列表。修改 nexus/server/internal/http/handler/cross_seed_handler.go、service/crossseed/{helpers,query,query_test,types}.go 和 nexus/webui/src/{components/settings/GeneralSettings.vue,stores/contentFilters.ts,views/CrossSeedDataView.vue}。种子维护及一键多种列表默认隐藏禁转、限转和分集条目。通用设置可分别打开限制类及分集显示并持久保存。列表筛选在分页前执行，默认设置不改变旧 API 客户端。显示禁转条目后仍不可批量选择或发布。
- 排查。152 日志显示 Harboring 2023 WEB-DL 2160p HEVC AAC-ZmWeb 的「重新获取」于 12:30:45 发起。当时另外一项任务在生成 12 张候选图，两次等待飞牛领取均超过 6 秒并返回 503，当前种子未产生图片。前一个任务 12:31:32 完成。按用户要求使用现有截图入口，先前的独立截图按钮改动未纳入本次镜像。未主动重试真实截图、上传 Pixhost 或修改飞牛，未验证重试能否生成图片。
- 验证。152 上无外网 Docker 全量 Go 测试及 Vue 类型检查和生产构建通过。首次 Go 回归夹具使用了额外未识别标签，修正验证数据后通过；独立容器测试改用隔离的数据挂载，按设置接口的实际响应重新验证。隔离 API 验证了权限、两个独立开关、分页、只读显示及保存；Edge 浏览器对已构建页面验证默认隐藏、保存和刷新后读取。生产健康接口及种子维护页面均返回 HTTP 200。
- 部署与 Git。152 的 STARPT 已更新至 ptmove-nexus:0.3.1-starpt.3，镜像 ID sha256:bee04f368095be95fd040693f574e15d97495426114f364182bbdb9e3d4b5bb9。现有数据继续挂载，旧镜像留作回滚。上线前确认发布及自动化队列为空、自动调度关闭。已更新 docs/DEPLOYMENT.json 及服务器部署记录。实际发种与 81 操作均为零。本轮未提交、推送、发布或导出附件。

## PTM-20261121-044 — 2026-11-21T18:41:12+08:00 — Codex root，飞牛刷流机中文路径 MediaInfo 提取

- 原因。152 日志显示 Harboring 的截图请求能命中远端视频并生成图片，MediaInfo 请求到达飞牛后返回 500。152 的本地路径回退报 /keep/TEST 不存在，掩盖了代理错误。在飞牛容器中复现：默认 POSIX 语言环境下，MediaInfo 对含中文的文件路径只返回一个换行。C.UTF-8 下同一文件可正常分析。ffprobe 的默认媒体摘要输出在 stderr，原回退只取 stdout。
- 修改。nexus/proxy/Dockerfile.nas 为 NAS 镜像设置 LANG=C.UTF-8。nexus/proxy/runtime_helpers.go 读取 ffprobe 成功返回的 stdout 或 stderr。nexus/proxy/runtime_helpers_integration_test.go 覆盖中文文件路径和回退。保留挂载和访问限制。docs/DEPLOYMENT.json 及 152 的部署记录更新。
- 验证。152 无外网 Docker 中代理全量 Go 测试与 go vet 通过。首轮镜像测试有一项因缺少 opt-in 跳过，补齐开关后重建并通过中文路径提取、ffprobe 回退和合成视频的截图预览。飞牛容器内 MediaInfo 接口实测返回 General、Video、Audio。调用 STARPT 原生 MediaInfo 刷新接口，经既有 HTTPS 领单取得 2715 字节，已写回该条织梦草稿，数据库状态为 completed，截图字段比对一致。实际视频为 HEVC、3840×1736，音频 AAC LC。
- 部署。152 构建 linux/amd64 媒体镜像 ptmove-media-agent:0.3.1-mediainfo.1，飞牛用同一 SHA256 的二进制升级了媒体容器。飞牛镜像 ID sha256:2e7aa2a332884ac57a76b7e85bc40b1bf3965acc9cf27cdb1c3e7c1acf43e637，原镜像可用于回滚。152 STARPT 本体和领单容器继续运行，健康接口 HTTP 200。以独立飞牛目标 SSH 主机密钥核验管理连接，未登录或更改 81 本机。未发种或上传图床。本次未实测 ARM、未提交、推送或发布。

## PTM-20261121-045 — 2026-11-21T18:55:09+08:00 — Codex root，转种准备自动补全 MediaInfo

- 准备流程。nexus/server/internal/service/migrationflow/{prepare_mediainfo,db_seed_query,fetch_store,publish_queue,automation_flow}.go、processing/{media, persist} 及 downloaderclient/client.go 接入缺失检测。已存在可用的 MediaInfo、BDInfo 或 ffprobe 报告时不请求飞牛。缺失时按种子 infohash 查询已完成 qB 条目，使用实际内容路径经原有 HTTPS 领单。找不到独立内容路径时保留缺项，禁止扫描共用保存目录。源站缺报告且同载荷已有完整报告时沿用。受限和正在执行 BDInfo 的条目不启动媒体提取。
- 保存与入口。repository/prepare_mediainfo_repository.go 以原种子键和旧媒体文本、旧状态为条件回填，保留并发编辑。bootstrap/app.go、http/handler/migrate/db_seed_publish.go、processing/persist/db_seed_query_entry.go、webui 的 cross-seed/panel/seedFlow.ts 加入授权的单种准备入口。已有的只读资料接口和维护页仍可正常查询。单种补全失败保留草稿并提示；批量和自动任务对代理提取失败分别记录跳过或未就绪。发布保护和调度开关按原设置执行。
- 验证。152 隔离 Docker 的全量 Go 测试、Vue 类型检查和生产构建通过。测试覆盖完整、空白及占位报告、受限、下载未完成、失败和并发回填。首轮 Go 构建发现维护接口参数签名回归，恢复原签名后通过。隔离验收首轮修正 SQLite 保留字的引用；另一轮收到补全警告，未保留具体诊断，追加诊断后通过。仅将 Harboring 对应草稿和 qB 缓存条目写入临时数据库，网络节制为 152 的领单入口。缺失报告经飞牛返回 2715 字节且回填至草稿和响应。关闭测试容器到领单入口的访问后，已有报告原样复用。隔离发布队列、发布日志、自动任务均为 0。线上同一条草稿正常载入，前后数据库行及任务计数一致。
- 部署及 Git。152 的 STARPT 更新为 ptmove-nexus:0.3.1-starpt.4，镜像 ID sha256:d625e147738c03ef351349b87f569b36fed886fb6ce6435d1e81fe0de53141bb，旧镜像留作回滚。健康接口及新资料准备接口均返回 HTTP 200，线上前端包含新接口。docs/DEPLOYMENT.json 与服务器部署记录已更新。飞牛继续使用现有 HTTPS 领单容器，本轮未重新截图、发种或操作 81。未提交、推送或发布。

## PTM-20261121-046 — 2026-11-21T19:28:40+08:00 — Codex root，媒体客户端 Web 连接设置

- 功能。nexus/proxy/pull 增加 admin.html、admin_server.go、admin_link.go 与对应测试，修改 agent.go、Dockerfile，新增 compose.agent-admin.yaml。页面支持 IP 或域名与端口、更新令牌、上传或粘贴 PEM 证书、选择系统 CA，保存前先无副作用验证 TLS 和令牌。设置经原子操作写入权限 0600 的独立文件，失败保留旧连接，领单通道在当前请求结束后读取新配置。旧租约结果交回原服务端。界面无令牌回显，只在本机可访问并要求独立密码。
- 验证。152 上无外网 Docker 全量 Go 测试和 go vet 通过。覆盖双模拟 TLS 服务端切换、坏令牌和证书、重启读回、旧租约回传、独立认证及跨来源请求拒绝。Edge 桌面和手机验证地址和端口表单、IPv6、证书上传、失败保留、重新载入及状态提示。首轮构建期间另一代理保存了修改，源文件核对未收口，收敛后重建并比对交付源码。飞牛实际验证需认证登录、错误令牌拒绝、原连接测试正常以及配置已持久化。用两组模拟服务端验证了实际切换，未切走生产连接。
- 部署及 Git。镜像 ptmove-pull:0.3.1-pull.4 在 152 构建后经 SHA256 校验部署至飞牛，飞牛运行镜像 sha256:1cc6479428ae6ae06191370fc33f8873678e4da0a35ea6adc047cddc715d5c78，管理页仅监听飞牛环回 19447。152 中转 Hub 与 STARPT 本体继续运行。升级后 qB 版本和媒体通道探针通过，发布及自动任务计数比对一致。docs/DEPLOYMENT.json、docs/REMOTE_SCREENSHOTS.md 和 152 部署清单已记录。只核验独立飞牛 SSH 目标，未登录 81 本机，未发种、截图或上传图床。本轮未提交、推送或发布。

## PTM-20261121-047 — 2026-11-21T19:34:05+08:00 — Codex root，撤回客户端管理页临时转发

- 用户先要求从本机访问飞牛客户端设置页。初次连接到经主机密钥核对的独立飞牛目标，现有 SSH 配置拒绝 TCP 转发。为当前运维用户和 SSH 端口添加仅允许本地转发至 127.0.0.1:19447 的规则，验证语法、范围后重载。此时用户取消转发，立即停止本机辅助进程，并撤回飞牛的临时规则。
- 撤回验证。本机 127.0.0.1:19447 已无监听，临时脚本和日志已清理。飞牛临时配置已移除，sshd -t 通过，重载后 ssh.service active，原有 allowtcpforwarding no 恢复。未连接 81 本机，无 Docker、发种或媒体操作。本轮未提交、推送或发布。

## PTM-20261121-048 — 2026-11-21T19:45:13+08:00 — Codex root，飞牛媒体客户端局域网直连

- 实现。nexus/proxy/pull 的 admin_server.go、admin_server_test.go、admin.html 和 compose.agent-admin.yaml 支持显式绑定指定私有 IPv4 和来源子网，默认仍为本机回环。飞牛部署为 192.168.31.238:19447，来源限于 192.168.31.0/24。保留独立密码、真实连接源、Host、Origin 和跨站请求校验。更新 docs/REMOTE_SCREENSHOTS.md、docs/DEPLOYMENT.json 和 152 部署记录。
- 验证。152 上无外网 Docker 全量 Go 测试及 go vet 通过。飞牛的实际监听只在该局域网 IP，登录 HTTP 200，未登录 HTTP 401，网段外和错误 Host HTTP 403，HTTPS 领单测试成功。当前 Windows 在 192.168.3.x，直接请求未收到 HTTP 响应，尚未从另一台同网段物理设备验证。原管理密码和连接文件按字节核对一致。
- 部署。152 构建 ptmove-pull:0.3.1-pull.5，按归档校验和部署至飞牛，运行镜像 sha256:6192c84a4f9e625058cd06b752d437ff0129bdeea4885190d2bfe79193e208c7。首次上线时部署脚本按镜像索引和挂载顺序的严格比较回滚，调整为核对运行镜像和挂载内容后重新部署通过。152 的 qB 版本查询继续通过。无 SSH 转发、无 81 本机操作，未实际发种、提交、推送或发布。

## PTM-20261121-049 — 2026-11-21T20:04:21+08:00 — Codex root，交付飞牛客户端统一 Compose

- 新增 nexus/proxy/compose.client.yaml。一份配置管理领单和媒体两个服务，默认沿用飞牛的两个已加载镜像、媒体目录、凭据文件和配置状态目录。管理页绑定飞牛局域网地址，媒体接口只绑定本机，视频目录只读。镜像按本地标签使用。
- 152 上使用假令牌执行 Docker Compose config，验证默认配置以及回环地址和媒体目录覆盖值。仅验证配置解析，没有创建或启动容器。已知切换现有飞牛时旧服务占用相同端口。本轮未访问飞牛或 81，未部署、提交、推送或发布。

## PTM-20261121-050 — 2026-11-21T20:46:21+08:00 — Codex root，客户端空配置启动及页面登录

- 统一部署。修改 nexus/proxy/compose.client.yaml、main.go、media_token.go 及测试、pull/Dockerfile、pull/go.mod。统一 Compose 使用两容器及仅供本地应用的命名卷，新装无需提前填写领单地址、密码文件、证书或令牌。内部媒体令牌自动生成并只读共享给媒体容器。保留当前飞牛的默认管理地址和 qB 目录，可按主机调整。
- 页面和连接。修改 pull 的 agent.go、admin_link.go、admin_server.go、admin.html，新增 admin_auth.go、agent_media_token.go 及回归测试。页面登录以有过期时间的 Cookie 维持会话；新装必须先改默认密码才允许访问连接设置，改密后旧会话撤销。旧安装可继续沿用已有密码和连接状态。空配置不会轮询服务端；地址、领单令牌、证书和可选 qB Token 在 Web 中设置，通过 HTTPS 校验后保存，配置随容器重建保留。证书自动申请此次未实现。更新 docs/REMOTE_SCREENSHOTS.md。
- 验证。152 隔离 Docker 的领单和媒体 Go 全量测试、go vet、镜像构建均通过。编译期间纠正 Cookie 字段拼写。Edge 桌面及手机复现空配置时测试按钮错误可点击，修复后页面登录、未改密禁用配置、保存和退出流程通过。152 使用全新隔离 Compose、两个临时卷及本机模拟 HTTPS 服务验证首次改密、连接失败保留空配置、Web 保存、媒体角色实际接收并回传文件检查、可选 qB 通道及重建持久化。测试卷和容器已清理。媒体试验镜像在此前验证过的 FFmpeg/MediaInfo 基础镜像上替换二进制。
- 构建及部署。构建 ptmove-pull:0.3.1-pull.6 与 ptmove-media-agent:0.3.1-websetup.1，仅用于 152 隔离验证。现有飞牛与 152 生产容器未切换，未访问 81，未发布、提交或推送代码。

## PTM-20261121-051 — 2026-11-21T21:59:32+08:00 — Codex root，NovaHD 已发布种子编辑

- 功能。任务记录提供「编辑已发布」。nexus/webui/src/views/PublishLogsView.vue 与 PublishedTorrentEditDialog.vue 显示现状、飞牛生成草稿、Pixhost 缩略图及前后差异，确认后才能保存，跨记录请求互相隔离。nexus/server 的 handler、bootstrap、migrationflow、repository、publish/publishededit 和 downloaderclient 按目标站绑定账号核对发布者及表单，保留 NFO 和其余字段，通过现有 HTTPS 领单按 qB 哈希与路径读取已完成的文件，只追加去重截图或明确指定的 MediaInfo。预览含限时确认凭据，已确认的提交先写入原文及差异，同一目标版本仅提交一次，回读后才报告已验证。docs/API_CONTRACT.md、PROJECT_STRUCTURE.md、docs/DEPLOYMENT.json 同步记录。
- 检查。152 无外网 Docker 的 Go 全量测试、Vue 类型检查、新增界面 ESLint 与 Vite 生产构建通过，Vite 仍提示历史共享包体积。隔离容器健康且未鉴权返回 401。线上包的桌面与手机浏览器走通模拟生成、预览、确认保存和旧媒体请求隔离。初次隔离 Web 构建遇到 pnpm 工作区检测，改用镜像已有依赖直接运行。增量构建首次 Go 命令的 BusyBox 不支持花括号展开，改用完整路径后通过。
- 实站。152 以 NovaHD 自有记录 #226481 核验归属、成功读取原有 7 张图和 MediaInfo，真实只读预览 HTTP 200，回读资料原样。初次两项媒体请求收到缺文件错误。核实三条成功记录的 qB 路径在媒体容器中均不可见，已加前置文件检查，#226481 线上复查返回明确 HTTP 409。作为对照，已知可读的 Harboring 文件通过同一领单通道回传 2715 字节 MediaInfo 与 5 张截图预览。实站的新增截图与保存仍待可读的自有发布对应文件；无图床上传或站点写入，发布日志 7、发布队列 0、自动任务 0、外站编辑提交 0。
- 部署与 Git。152 首先健康运行 0.3.1-starpt.5，路径诊断及 Pixhost 实际域名兼容后更新为 ptmove-nexus:0.3.1-starpt.6，ID sha256:920770e082035bb439a3ccfad85c0e900b014a6dc7e3e182d77c4caa9cc3ac0c，18120 健康，保留 .4 回滚镜像。未重新发种、修改 qB 做种、访问 81 或升级飞牛。原主工作区改动保留，本次代码在独立工作区，未提交、推送或发布。
- 本地核对。恢复独立工作区 30 个由预检临时改动的跟踪构建产物，移除 32 个同路径临时产物。构建产物目录无 Git 变更，最终镜像和 152 验证结果保持已记录状态。

## PTM-20261121-052 — 2026-11-21T22:15:41+08:00 — Codex root，NovaHD #226481 实站编辑验证与封面核对

- 诊断。152 经现有 HTTPS 领单检查飞牛媒体容器，`/keep/TEST` 可读，qB 原记录仍指向该目录下已不存在的目标文件。按 qB 同名同大小记录在 `/keep/PTNexus` 找到用户确认移放的副本，实时核对文件名、4,831,475,403 字节及编码、时长、分辨率等媒体参数。
- 操作。飞牛回传新 MediaInfo 和 5 张低清候选。选出 3 张，确认 Pixhost 均返回 HTTP 200 PNG。STARPT 预览确认保留原简介和截图、更新 MediaInfo，再提交一次。保存返回 HTTP 200 verified，NovaHD 回读及独立详情页 HTML 检查确认新增 3 张截图和报告符合预览。审计记录 verified 一条，原发布日志、队列和自动任务计数维持 7/0/0。
- 封面。NovaHD 页首封面与简介首张图片使用同一 Pixhost 地址，直接 GET 在有无 NovaHD Referer 时均返回 302，目标为 `/image/removed.png`。对照截图返回真实 HTTP 200 PNG。只核对原因，站上封面链接保留。152 私有暂存文件已移除，本机 D 盘 5 张低清候选因清理命令受限保留在本次暂存目录。
- 影响。仅 #226481 既有发布资料发生变更，使用 152 线上既有镜像和飞牛领单。未重新发布、修改 qB 记录、访问 81、构建或部署镜像、提交或推送代码。仓库文件只追加本条变更记录。

## PTM-20261121-053 — 2026-11-21T22:31:33+08:00 — Codex root，NovaHD #226481 封面直链替换

- 原因与范围。Pixhost 展示页引用的 img3 原图仍可加载，NovaHD 保存的 img2 旧直链会跳转到占位图。经用户授权，通过 152 复用现有站点编辑表单解析、所有权检查与审计机制，仅替换 #226481 简介首张封面的图片主机。
- 验证。先确认预览仅有一处 img2 到 img3 替换，9 张截图和 MediaInfo 保留。复查站点版本后执行一次 takeedit，原站 HTTP 302 后回读确认整份表单的其余字段一致。NovaHD 详情页 HTTP 200，页首和简介内封面都引用 img3，旧直链不再出现。按 NovaHD 来源请求新直链返回 HTTP 200 和有效 JPEG。审计 #2 verified，发布日志、发布队列、自动任务计数仍为 7/0/0。
- 影响。152 上项目专用临时核验程序与构建目录已清理。未重新发种、修改 qB 或飞牛、访问 81、提交或推送代码、重建或部署服务。仓库只追加本记录。

## PTM-20261121-054 — 2026-11-21T22:49:46+08:00 — Codex root 与 cover_backend、cover_ui、cover_feature_review，封面编辑及任务记录布局

- 服务端。publishededit 的封面定位仅接受 NovaHD 头图与简介首图一致的发布记录。Pixhost 解析接受 HTTPS 展示页、图片直链或缩略图，限制域名与公网连接，重新验证图像类型及签名，失效旧直链可通过展示页找出可打开的原图。migrationflow 封面预览的签名同时绑定原始输入和解析结果，禁止混传截图或 MediaInfo，保存前重验证原图和站点版本，单次提交后校验头图及其余表单字段。
- 界面。PublishedTorrentEditDialog.vue 增加「更换封面」，展示原图差异并要求确认，切换记录时隔离迟到响应。非 Pixhost 旧封面可更换，当前图仅在站上详情页查看。PublishLogsView.vue 将日志、编辑和查看、删除排为两行，窄屏将状态和操作区分行。docs/API_CONTRACT.md 补充请求与响应；docs/DEPLOYMENT.json 与 152 元数据同步。
- 验证。152 无外网 Docker 的 Go 全量测试、Vue 类型检查、定向 ESLint 和 Vite 构建通过，两轮隔离容器鉴权均返回 401。首次构建移除未使用的导入后通过。桌面、平板和手机的浏览器模拟预览、确认保存、迟到响应及按钮边界通过。部署 .7 后视觉检查发现手机确认文案截断，CSS 更正后用 .8 复测通过。
- 实站与部署。152 正在健康运行 ptmove-nexus:0.3.1-starpt.8，ID sha256:42029995b55eb387295df9357688e208a506802735d83a0c4c9e84302f5a7768，保留 .7 回滚。真实 #226481 展示页、失效 img2 直链均识别为当前 img3 原图，替换预览 HTTP 200 只读通过，站点原快照未变，发布日志/队列/自动任务/编辑尝试维持 7/0/0/2。保存以模拟浏览器与适配器测试验证，未做真实站点写入、重新发种、操作 81、提交、推送或发布。

## PTM-20261121-055 — 2026-11-21T23:10:40+08:00 — Codex root，准备 STARPT v0.3.1-starpt.8 发行

- 从 152 已验收的 .8 构建目录核对并导入服务端与前端生产源码、部署所用 Web UI 构建产物。纳入用于复现该镜像的此前服务端基线，保留客户端和其他未上线工作区改动。本次发行新增 `compose.starpt.yaml` 和版本安装说明。
- 152 使用已验证 amd64 镜像以空数据目录、无配置文件及临时回环端口执行独立 Compose 启动和 HTTP 健康检查，测试服务已清理。现网仍运行镜像 sha256:42029995b55eb387295df9357688e208a506802735d83a0c4c9e84302f5a7768。152 导出完整归档的 SHA-256 已录入部署文档。
- 记录时准备提交、标签及两站发行，未上传附件或提交发行表单。未重新发种、访问 81 或更新现网服务。后续在独立记录核验各远端与附件。

## PTM-20261121-056 — 2026-11-21T23:28:40+08:00 — Codex root、gitee_publish、cover_release_image，STARPT v0.3.1-starpt.8 发行

- 提交 3df1b483cd7e69ed58c6a612f264b411e812daf5 与标签 v0.3.1-starpt.8 已推送到 Gitee 和 GitHub，两处远端已核对标签解引用与发布提交一致。原始发行标签保留；本记录更新 `CHANGELOG.md`、`docs/DEPLOYMENT.json` 和 `docs/RELEASE-v0.3.1-starpt.8.md`。
- GitHub 私有仓库发行已发布。API 确认完整 Linux AMD64 镜像、SHA-256 文件和 `compose.starpt.yaml` 均已上传，镜像 522,128,247 字节及摘要 900019a17ada63d2160d83a1f716fcdf35c69f97d0815569a1ecf7df4c7f494a 与 152 的导出一致。https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v0.3.1-starpt.8
- Gitee 发行 ID 1344453 已发布。Compose 附件 ID 3782964，508 字节，从发行接口回下载后比对 SHA-256 cfc417a0cf041063a206d1740ba51e0a033e857d1d85258bfda6424b31f649b7 通过。已校验增量包可从 v0.3.1-starpt.1 精确复原 .8 OCI 镜像。Gitee API 对该 99,020,403 字节附件返回 HTTP 400 和仓库附件 1 GB 配额错误，附件列表只含 Compose，发行正文提供 GitHub 完整镜像入口。https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.1-starpt.8
- D 盘历史发布 ZIP 包含标签源码、完整镜像、Compose、恢复说明及校验清单。ZIP 内容 CRC、SHA-256、标签提交元数据通过，归档库索引已追加。152 上项目容器状态为 healthy，本次 152 操作为 Compose 验收、镜像归档和健康状态复核。未重新发种、写入站点或访问 81。

## PTM-20261121-057 — 2026-11-21T23:55:15+08:00 — Codex root、mediainfo_flow、mediainfo_quote_parser、mediainfo_review、remove_upstream_refs，简介 MediaInfo 提取及来源文案整理

- MediaInfo。acquire/extract/description_quote.go 与现有抓取逻辑支持真实简介、带反斜杠的样例和 HTML 引用，校验容器及音视频结构，歧义引用回退。processing/persist 处理新抓取与维护刷新，migrationflow/prepare_mediainfo.go 处理缓存及飞牛回退；repository 更新校验此前的媒体、状态及简介。准备日志提示引用命中。
- 文案。README、docs、nexus/readme.md、站点配置、Web 导出名、更新文案、API 标题和健康接口清理可见上游引用。历史日志、兼容字段及既有用途限制保留。
- 验证。152 的独立无外网 Docker 运行样本、缓存优先、并发回填、HTML 引用、错误和歧义回退测试，Go 全量测试、前端类型、定向 ESLint 与 Vite 均通过。隔离 HTTP 准备接口 4/4，通过用户样本回填与原正文保留。隔离测试的 curl 和 fixture 时间字段问题已修正。生产健康和 Web 入口与构建一致，发布日志/队列/自动任务/编辑尝试计数为 7/0/0/2。线上镜像 ptmove-nexus:0.3.1-starpt.9，ID sha256:9c55641ccfaf4d344041bac7cc6e9c994ef607a316c8f3b9ec4042b7c1e953fa，回滚 ptmove-nexus:0.3.1-starpt.8。
- 本轮涉及 152 的项目容器和隔离验证，未重新发种、操作 81、提交、推送或发行新标签。飞牛回退通过模拟调用测试，样例命中简介无须真实访问飞牛。

## PTM-20261122-058 — 2026-11-22T00:04:52+08:00 — Codex root、starpt_ui_inventory、starpt_dashboard_ui、starpt_sites_ui，STARPT 工作台与站点统计可视化

- Web。App.vue、glass-morphism.scss、star-shell.scss 更新导航及语义色板。HomeView.vue 使用现有流量接口汇总已启用下载器近 24 小时或昨日的数据，提供分时柱状图、可展开明细、下载器状态和原入口。SitesView.vue 使用站点和官组接口呈现各站体积、种子数和前五对比，保留排序表格、筛选和扫描。容量按 IEC 单位显示，官组切换时隔离迟到数据。
- 验证。152 的离线 Docker 前端类型检查、定向 ESLint 和 Vite 构建通过。基于候选静态包用隔离浏览器样例验证 1440px 与 390px，两页渲染、图表数据、范围切换、小时明细、统计刷新、导航、空态和错误态通过，未出现页面异常或整页横向溢出。根据首轮截图调整移动卡片高度与容量单位后复测通过。
- 部署。现网镜像 ptmove-nexus:0.3.1-starpt.10，ID sha256:1c42d38cd28bf5e8cea6e201c7e621b3dcf2b2bfdeb97758d22f0476ccc81b5e。健康检查、入口静态文件 SHA-256 和发布/队列/自动化/编辑计数复核通过。回滚镜像 ptmove-nexus:0.3.1-starpt.9。本地 Web 构建产物和 docs/DEPLOYMENT.json 已同步。
- 本轮仅操作工作区与 152 项目环境，未重新发种、访问 81、提交、推送或创建新 Git 发行。

## PTM-20261122-059 — 2026-11-22T00:27:48+08:00 — Codex root、settings_layout、operations_theme、aux_theme、theme_review，STARPT 全站主题与设置滚动

- 设置。SettingsView.vue 与六个设置组件统一布局。分类导航在窄屏换行，设置工作区承担纵向滚动，下载器卡片、长表单和列表可达，弹窗正文滚动及操作页脚分开。旧实时流量开关的显示随独立页面移除。
- 主题。App.vue、index.html、main.ts 与共享样式使用深海军蓝、紫蓝强调色和暗色组件变量。登录、插件、种子列表、批量获取、维护、任务日志、预览、更新和弹窗同步颜色、边框及状态。
- 流量入口。router/index.ts 把 /info 跳转到工作台，App.vue 与 HomeView.vue 去除独立入口，删除 InfoView.vue。工作台按原流量 API 展示图表。
- 验证。设置和全局类型检查、28 个目标文件的 ESLint、152 离线 Vite 构建通过。首轮发现的站点表单重复 class、两处残留浅色及临时 pnpm 工作区文件均已处理。最终构建使用隔离浏览器遍历主要页面和六个设置页，并验证 1440/1024/390/320 像素下载器页面、多卡片滚动和十行路径映射弹窗的底部按钮。旧 /info 跳转及工作台流量图通过。
- 152 部署 ptmove-nexus:0.3.1-starpt.11，ID sha256:78b13e80be7089f5e8306fb211a4d308ca7c588af0a5280f020e1c586cf73eb9，回滚 ptmove-nexus:0.3.1-starpt.10。线上健康、静态入口摘要及发布计数通过。同步 docs/DEPLOYMENT.json 和 Web 静态产物。未重新发种、访问 81、提交、推送或创建 Git 发行。

## PTM-20261122-060 — 2026-11-22T08:25:20+08:00 — Codex root、release_version、release_runtime_audit，STARPT 1.0.0 版本与镜像

- 发布范围包含简介 MediaInfo 自动提取、工作台图表、全站深色主题及设置滚动修复。nexus/VERSION、服务端版本接口、Dockerfile、两份 Compose 更新到 1.0.0。部署脚本按版本文件验证健康返回，保留旧实例回滚逻辑。当前发行文档使用 Gitee 仓库的 canonical 地址。
- 验证。Go 全量测试和版本路由回归在 152 通过，Python 发布保护测试 5 个通过。1.0.0 镜像沿用通过桌面与移动端验收的 Web 产物。独立 Compose 空数据目录验证启动、健康与版本，测试容器已清理。
- 152 已部署 ptmove-nexus:1.0.0，镜像 sha256:1c166c1ec5ac61805c90f90217d159a157aa3b2f3090514b8b405f18475bdcac，回滚 ptmove-nexus:0.3.1-starpt.11。生产入口摘要匹配，健康与发布/自动化计数复核通过。完整镜像导出为 starpt-v1.0.0-linux-amd64.tar.gz，543336327 字节，摘要 8cd7616e6fce00b05858111a145fb4c4ca1409e12497f3bdb8223d10a1e148ac。
- 准备提交和标签时，镜像传输与 GitHub/Gitee 发行仍在进行。Gitee 原有附件占用约 1.013 GB，完整镜像规划附于 GitHub 私有仓库。未重新发种或访问 81。

## PTM-20261122-061 — 2026-11-22T08:40:54+08:00 — Codex root、release_gitee_100、release_image_archive_100，STARPT 1.0.0 双仓发行与归档

- 源码、已验证的 84 个 Web 构建产物与 Compose 均包含在 162ebf2a16ae476dc2c0bda27df981fa9c2fb515。Gitee 和 GitHub 的 v1.0.0 标签解引用指向同一提交。GitHub 私有仓库发行已正式发布，三个附件状态及 SHA-256 索引均已核对。完整 Linux AMD64 镜像 543336327 字节，SHA-256 8cd7616e6fce00b05858111a145fb4c4ca1409e12497f3bdb8223d10a1e148ac。https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.0
- Gitee 使用 canonical 仓库 star1999/starpt，发行 ID 1351271。Compose 与镜像校验文件回下载后逐字节匹配。仓库附件余量不足以镜像完整文件，发行正文链接同版本 GitHub 私有发行；保留所有旧附件。https://gitee.com/star1999/starpt/releases/tag/v1.0.0
- D 盘历史发布 ZIP 包含标签源码、完整镜像、Compose、恢复说明、校验清单。SHA-256 6d685f8763c8478155671ed6518ad142e83d62730a3661590c9d06c9699ddb3a，以 OCI 摘要链、各成员哈希及 ZIP CRC 验证并更新 INDEX.jsonl。镜像复用 152 已验收 UI，独立 Compose 与线上版本接口通过。此发行记录另行提交，版本标签固定。未发种或访问 81。

## PTM-20261122-062 — 2026-11-22T11:45:15+08:00 — Codex root、speed_inventory、speed_live_backend、speed_panel，工作台实时速度

- HomeView.vue 和 HomeSpeedPanel.vue 将速率图接回工作台。显示所选下载器的上传和下载速度以及近 60 秒趋势，右上角可切换下载器。可见时每 2 秒刷新，切换时使用所选下载器的数据，离开时停止。保留流量总量和趋势卡片。
- internal/bootstrap/app.go、internal/service/stats_service.go、internal/service/stats/{types,speed}.go 和 internal/service/tracker/{tracker,tracker_test}.go 复用采集线程最近 120 秒的内存样本。现有近期速率接口合并数据库和内存，同秒同下载器以内存优先。sampled 区分失采和实测零速率。保留已有采集设置和批量写库行为。
- 152 离线构建通过 Go 两项速率回归及全量测试、Vue 类型检查、目标文件 ESLint 和 Vite 构建。以 1440/390 像素浏览器测试两台模拟下载器、切换、跨午夜、失败恢复、退出停止和空态。连续请求间隔为 2015、1986、2003 毫秒。Vite 仍有包体积提示。
- 152 部署 ptmove-nexus:1.0.0-speed.1，镜像 sha256:c3ffda1d6350296205994d4c324d3f52f5b8917b8424986e3e41836679d54796，回滚镜像 ptmove-nexus:1.0.0。线上健康和公开入口摘要核对通过。匿名速率接口返回 401，实际下载器速率未做登录核对。发布、队列、自动化和编辑计数无变化。同步 docs/DEPLOYMENT.json 与 41 个 Web 产物。本轮未重新发种、访问 81、提交、推送或发布新版本。

## PTM-20261122-063 — 2026-11-22T12:01:06+08:00 — Codex root、speed_dropdown_review，工作台下载器下拉框

- HomeSpeedPanel.vue 用站内 Element Plus 下拉框替换浏览器原生 select，沿用 STARPT 深色主题。菜单锚定控件，单下载器时可展开查看，空列表时禁用。切换与 2 秒速率轮询继续使用原组件逻辑。
- 152 离线 Vue 类型检查、目标 ESLint 和 Vite 构建通过。隔离 Edge 浏览器验证 1440/390/320 像素暗色菜单、触屏切换、选中后速率刷新、键盘关闭以及单项与空态。移动端使用浏览器触控模拟，未检查实体设备。
- 152 部署 ptmove-nexus:1.0.0-speed.2，镜像 sha256:1efe48eb62f7298825847bd822c28a1776a873d31e463329e86907ab1e074917，回滚镜像 ptmove-nexus:1.0.0-speed.1。线上健康、公开入口摘要和发布计数核对通过。复用前版服务端二进制，同步 docs/DEPLOYMENT.json 和 Web 构建产物。未重新发种、访问 81、提交、推送或创建 Git 发行。

## PTM-20261122-064 — 2026-11-22T12:08:42+08:00 — Codex root、v101_metadata、v101_artifact_review、gitee_v101_preflight，STARPT 1.0.1 发行准备

- 将工作台实时速度和下载器深色选择器纳入正式发行。nexus/VERSION、Go 版本与更新记录、镜像标签、Compose 和部署脚本测试版本统一为 1.0.1。保留已验收的 41 个 Web 构建文件。README 和 docs/RELEASE-v1.0.1.md 提供该版安装入口。根目录和 nexus 的 AGENTS.md 记录后续验证通过后自动提交和双仓发布的授权。
- 152 通过 Go 全量测试、速率和版本接口检查。部署脚本本机测试 5 项通过。独立 Compose 使用空数据目录，已核对健康、版本和 Web 入口摘要。线上部署 ptmove-nexus:1.0.1，镜像 sha256:a344ecd710505f813620508cced50ecf30af62bc018c179ba1486f8d19025003，回滚 ptmove-nexus:1.0.0-speed.2，健康和发布相关计数核对通过。
- 152 已导出完整 Linux AMD64 镜像 starpt-v1.0.1-linux-amd64.tar.gz，581373136 字节，SHA-256 8206e95d634488b75940a9cdcc2e806146ec7da743c9e5227f92713f41ca75d0。组装提交时，双仓推送、附件传输和历史归档待完成。Gitee 单个附件受平台限制，现有附件约 1.01 GB，计划保留历史并在 Gitee 提供 Compose、校验文件和 GitHub 完整镜像入口。未重新发种或访问 81。

## PTM-20261122-065 — 2026-11-22T12:23:37+08:00 — Codex root，STARPT 1.0.1 双仓发布与归档

- v1.0.1 的代码、41 个经验证的 Web 文件和 Compose 已推送到 Gitee 与 GitHub。标签解引用均指向 1ddedde0526814553fa67435200621150bc063fc。GitHub 私有发行的三个附件已发布并核对大小与 SHA-256。完整镜像 581373136 字节，SHA-256 8206e95d634488b75940a9cdcc2e806146ec7da743c9e5227f92713f41ca75d0。https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.1
- Gitee 发行为 1351914，Compose 和 SHA-256 文件已上传并回下载核对。完整镜像超过 Gitee 单附件上限，仓库历史附件约 1.01 GB。发行页链接同版本 GitHub 私有发行中的完整镜像，保留历史附件。https://gitee.com/star1999/starpt/releases/tag/v1.0.1
- D 盘 ZIP 包含同一标签源码、可独立装载的完整 Linux AMD64 镜像、Compose、恢复说明和校验清单。ZIP SHA-256 ee4b1d9b7239f1bcd2264fc3ccc14d75558e3563617ed721ca383adbdd72ee0d，验证了 OCI 摘要、ZIP 成员哈希及 Compose 与标签源码的字节一致，档案已索引。未重新发种或访问 81。版本标签固定，发行记录另提交并推送。

## PTM-20261122-066 — 2026-11-22T13:03:54+08:00 — Codex root、automation_policy_inventory、automation_source_filters、automation_download_history、automation_rules_ui_contract，自动化官种和历史下载硬性过滤

- acquire/automation 与 processing/automation 实施官种门槛与禁转、分集、已下载排除。织梦筛选 `tag_id=3`，按 `sort=5&type=desc` 排序，并用站内整数体积区间和做种/下载人数缩小范围。逐条核对官方标签、完整载荷字节和目标查重，合格行体积降序。同一登录账号的灰色 inactivity 100% 和绿色 seeding 100% 均由侧栏只读核对。更正后的实样本使用页面证据验证，未写入个人种子编号。
- migrationflow、automation_repository 与 downloaderclient 核对跨月和跨目标的自动任务历史、qB 哈希、查重和租约。读取失败拒绝执行，下载前和发布前重新核验，隔离回归覆盖本任务自己的完整下载、目标新增重复、同路径已存在、并发预留和无副作用预览。
- AutomationSettings.vue、机器 OpenAPI 和 API 文档展示固定筛选规则、跳过原因与禁选。旧偏好键兼容读取，不改变规则。初期人工补录草稿已撤销。152 Go 全量、Vue 类型检查、ESLint、Vite 和本机部署脚本 5 项测试通过。首轮一个测试文案预期已修正；最终 1440/390 像素隔离浏览器预览通过。
- 152 以空目录验证独立 Compose，生产 ptmove-nexus:1.0.2 健康，版本及入口摘要核对通过，回滚 ptmove-nexus:1.0.1，发种和自动化计数无变化。同步 41 个 Web 文件、README、版本发布说明和部署记录。152 完整镜像 600401955 字节，SHA-256 0d325567f2cd4fd725bc58093b99b6b6f9d4e2f637ef9f489c16e7fc3f735edc。组装提交时双仓发布、附件和 D 盘归档待完成。未操作 81、未实际下载资源或发种。

## PTM-20261122-067 — 2026-11-22T13:12:48+08:00 — Codex root，STARPT 1.0.2 双仓发布与归档

- v1.0.2 的代码、41 个经验证的 Web 文件和 Compose 已推送到 Gitee 与 GitHub。标签解引用均指向 4c24afe913b4afd003d397753c8f919404642d29。GitHub 私有发行的三个附件已发布并核对大小与 SHA-256。完整镜像 600401955 字节，SHA-256 0d325567f2cd4fd725bc58093b99b6b6f9d4e2f637ef9f489c16e7fc3f735edc。https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v1.0.2
- Gitee 发行为 1352048，Compose 和 SHA-256 文件已上传并回下载核对。完整镜像超过 Gitee 单附件上限，仓库历史附件约 1.01 GB。发行页链接同版本 GitHub 私有发行中的完整镜像，保留历史附件。https://gitee.com/star1999/starpt/releases/tag/v1.0.2
- D 盘 ZIP 包含同一标签源码、可独立装载的完整 Linux AMD64 镜像、Compose、恢复说明和校验清单。ZIP SHA-256 950c84b00af0a9429fa2f077d39aa83819eb2edda1053a2b00f875c17d74a804，验证了 OCI 摘要、ZIP 成员哈希及 Compose 与标签源码的字节一致，档案已索引。未重新发种或访问 81。版本标签固定，发行记录另提交并推送。

## PTM-20261122-068 — 2026-11-22T21:13:36+08:00 — Codex root、zmpt_seeder_fix、seeder_release_metadata、zmpt_fix_review，织梦做种人数与分页校验

- `acquire/automation` 从同一种子 ID 的列表行及 `#seeders` 链接读取做种人数。保留确认为零的值，缺失或格式异常时读取详情。预览及执行前复查统一使用该规则。体积上下限和降序从匹配区间第一页开始，沿用分页和详情检查上限。`docs/API_CONTRACT.md` 记录该行为。同步版本接口、`VERSION`、Dockerfile、两份 Compose、README 和 v1.0.3 发行说明。
- 回归测试覆盖做种与下载、完成计数的区分、千位分组、零值、错误行、详情回退及第二页。先修正格式校验后，152 Go 定向与全量测试、部署脚本 5 项检查、独立 Compose 以及线上 1.0.3 健康通过。生产镜像 `ptmove-nexus:1.0.3`，回滚镜像 `ptmove-nexus:1.0.2`。复用已验证的 Web 文件。
- 152 读取织梦经筛选的第 1、2 页，各返回 100 条且无重复 ID。实站 6–7 GiB、至少 3 人做种预览返回 100 条。50 条已核验体积的结果均满足做种人数。NovaHD 查重判定 37 条已存在、13 条未知，最终 0 条可选。两条指定样本均识别为已下载而跳过。设置、任务、配额和发种记录计数保持一致。未实际下载资源、发种、操作 81。
- `docs/DEPLOYMENT.json` 记录部署、回滚和预览证据。发布用完整 Docker 归档解压后与 152 的 Docker 导出逐字节一致。归档 653264101 字节，SHA-256 为 `baa62d9c0e47f1537e441963153163c2c6b827901ab362d145419d6d74f9bd37`。记录时双仓发布与历史 ZIP 待完成。

## PTM-20261122-069 — 2026-11-22T21:23:57+08:00 — Codex root，STARPT 1.0.3 双仓发行及 NovaHD 查重复核

- v1.0.3 标签在 Gitee 和 GitHub 均指向 `4d40f3f495f06ae7e415c43745c017188e2296a2`。GitHub 私有发行已发布，完整镜像、Compose 和校验文件的服务端 SHA-256 均匹配。Gitee 发行 1354186 已上传 Compose 和校验文件，回下载比对通过，完整镜像链接同版 GitHub 私有发行。
- D 盘历史 ZIP 包含标签源码、完整 Docker 镜像、Compose、恢复说明和校验清单。ZIP SHA-256 为 `d0f5d9838647aeea70ba8306f6cb1c527ba83ea5b08387a665c01bdb9e9c5c33`。验证 50 个 OCI 对象、44 个 Docker 层、原始 tar 哈希、ZIP 成员和标签 Compose 一致，项目索引已更新。
- 只读复核确认 NovaHD 旧查重会把推荐区和用户详情链接计为命中，部分结果还混有不同版本。37 条为程序标记，不能验证实际对应版本。将已知问题补入 `docs/RELEASE-v1.0.3.md`、`docs/DEPLOYMENT.json` 和两站发行说明。未再触发预览、下载或发种，未操作 81。版本标签保持原提交，本记录单独提交并推送。

## PTM-20261122-070 — 2026-11-22T22:25:19+08:00 — Codex root、novahd_exact_duplicate、automation_client_media、automation_outcomes_ui、automation_104_review，自动转种查重、补资料和结果登记

- NovaHD 查重读取实际搜索结果，包括断种，并在扫描、下载前和发布前校验发行和载荷。明确空结果可通过，分页或不完整证据保留待核对。织梦静态辅种链接可以定位同源 NovaHD 候选。未新增 IYUU 外部查询。
- qB 确认完整后按冻结的哈希和文件清单补 MediaInfo、截图，复用已有有效资料，优先从简介提取媒体报告。使用既有 HTTPS 飞牛客户端和 Pixhost。媒体回填受任务租约和并发编辑校验保护。有效自动截图可继续，人工待确认的草稿状态保留。客户端不可用时按两分钟间隔重试。
- 「已存在」成为终态，保留已用下载额度且不计成功。区分待核对、待补资料和已发布。原子登记结果、取消发布前队列，并识别旧回执中明示的重复。保留上传结果不明时的核对和禁止盲目重试。
- 152 Go 全量、Vue 类型检查、定向 ESLint、Vite 构建和独立 Compose 均通过。修正测试中的回填退避和媒体字段夹具，覆核取消竞态、旧重复回执与同载荷不同年份。5 项部署脚本测试通过。同步 41 个实际 Web 产物、版本、Compose、API 合约与使用说明。
- 152 健康运行 `ptmove-nexus:1.0.4`，镜像 `sha256:9562557d072df7f27562d2752cf7f317778b0c2b7582144c1d5aee1939395013`，保留 1.0.3 回滚。实站只读预览 100 条中 20 条通过、12 条确认已有、18 条待核对。两条指定历史下载均跳过，设置、任务、配额、发种和编辑计数一致。未下载实际资源、发种或操作 81。自动化开关保持关闭，补资料和发布结果处理通过隔离夹具验证。
- 发行用完整 Docker 归档 673404753 字节，SHA-256 为 `e59f5fb5bee852ce465ded12d0e9c418b5ad6ffea2804f97ad6b1ee3249f11a4`。归档解包结果与 152 Docker 导出哈希一致，53 个 OCI 对象及 Compose 已核对。此记录编写时，双仓提交、标签、发布附件和历史 ZIP 待完成。

## PTM-20261122-071 — 2026-11-22T22:49:54+08:00 — Codex root，STARPT v1.0.4 双仓发行和归档

- GitHub 和 Gitee 的 v1.0.4 标签均指向 `58fd5f16692fd1fb09f7c1f2381855bc9a526c39`。桌面更新中断的 GitHub 上传改用 IPv4 重新完成，已核对完整镜像、Compose 和校验文件的大小及 SHA-256，发行已公开给私有仓库的成员。
- Gitee 发行 1354564 已发布。Compose 和校验文件回下载后逐字节匹配本地发布材料，完整镜像入口指向同版 GitHub 私有发行。发布证据写入 `docs/DEPLOYMENT.json`。
- D 盘历史 ZIP 已完成，包含标签源码、完整镜像、Compose、校验和恢复说明。核对原始 Docker tar、OCI 对象、ZIP 成员及标签 Compose；ZIP SHA-256 为 `2a91c915a04980b929a96ae593f332077ba0bd7992fcd46e3d44211902027aee`，已登记项目索引。
- 重启后复核 152 仍健康运行 v1.0.4。自动化开关保持原设定，保留 1.0.3 回滚镜像。未重复运行实站预览，未执行真实下载、发种或操作 81。版本标签保持原提交，本条记录独立提交。

## PTM-20261122-072 — 2026-11-22T23:17:10+08:00 — Codex root、seed_lookup_flow、zmpt_scanner_tests，织梦动态辅种查重

- `acquire/automation/scanner.go`、`zmpt_crossseed.go` 与 `docs/API_CONTRACT.md` 将织梦扫描和下载前查重接到详情页动态的「辅种」接口。目标域名命中即跳过，成功列表未列出或接口明确返回无辅种数据时通过。未加载、超时和无法解析的响应保留待核对。官种、禁转、分集及下载记录仍先筛选。发布前继续查询目标站，其他源站沿用既有流程。
- 从 152 用 HTTPS GET 只读核对织梦页面和动态响应。#140457 明确列出自由农场、未列出 NovaHD，返回 7 个不同站点。对保留的全零合成哈希核实了明确无辅种响应结构。NovaHD 命中、超时、未加载和发布前新增重复用隔离夹具验证。
- `scanner_test.go`、`zmpt_crossseed_test.go` 和 `novahd_duplicate_test.go` 覆盖动态命中、已知未命中、未知结果和三阶段查重边界。修正测试夹具的字节类型和分页计数后，152 无外网 Docker 容器的 `go test ./...` 全部通过，共 26 个带测试的包。回传 gofmt 结果，`git diff --check` 通过。独立测试暂存清理命令被运行策略拦截，留存的仅为源码与测试文件。
- 按本轮只读核验要求，改动保留于独立工作树，Git 提交、推送、生产部署和发布均未执行。未下载实际资源、发种或访问 81。

## PTM-20261122-073 — 2026-11-22T23:49:29+08:00 — Codex root、iyuu_batch、batch_store、dup_qb、iyuu_batch_review，织梦分批下载与 IYUU 查重

- `acquire/automation`、`migrationflow/automation_*` 和 `torrentdata/iyuu*` 接入织梦辅种预查、完整下载后的单次 IYUU 哈希批查、明确未命中才准备资料及发布。源站发布前限制和完整载荷复核继续执行。未识别结果保留等待并退避重试。新织梦运行默认每批 20 个，按剩余名额缩小批次，处理完成后重新扫描。补扫在详情读取前跳过账本已处理的条目。
- `repository/automation_*` 持久化批次、结果、处理尝试和发布确认时间。月度体积限制取消，单种体积和磁盘空间检查继续执行。月度统计以已确认发布为准，未决上传保留名额，重复及确定失败释放名额。复核并修复扫描恢复、跨月、手动重试及关闭模块后的批次处理。
- `downloaderclient/client.go` 与 `automation_duplicates*` 按任务标签、内容身份、路径及租约核对后处理 qB 自有下载。默认保留。移动到指定目录下的任务子目录，删除只移除 qB 记录并保留文件。提交前重查设置、在数据库登记一次性尝试，异步移动只读复核。`AutomationSettings.vue`、OpenAPI 和 `docs/API_CONTRACT.md` 同步批次、重复处理及新配额界面。
- 152 在隔离容器执行全量 `go test ./...`，26 个有测试的包通过。测试覆盖批次等待、混合查重结果、配额并发、跨月、恢复以及 qB 操作的所有权与路径检查。前端定向 ESLint、`vue-tsc`、Vite 构建通过，Vite 报大分块提示。已同步 gofmt 和格式化后的 Vue 文件，`git diff --check` 通过。浏览器截图与交互检查的启动命令被运行策略拦截，未取得桌面或窄屏渲染证据。
- 改动及构建验证保留在隔离环境。未执行 Git 提交、推送、生产部署或版本发行；未调用真实 IYUU 或 qB，未实际下载、移动、删除、发种或访问 81。

## PTM-20261123-074 — 2026-11-23T00:23:34+08:00 — Codex root、home_progress、small_first、release_readiness，STARPT v1.0.5 小种优先及工作台进度

- `processing/automation`、`acquire/automation`、`automation_repository.go` 和 OpenAPI 解除旧月度体积设置，仅忽略旧客户端上传值。织梦查询及候选排队按完整体积升序，待下载项按体积派单，保留 qB 并行能力和单种体积区间。纳入待发布的织梦动态辅种、IYUU 批查和成功次数配额。
- `HomeAutomationProgress.vue`、`HomeView.vue`、`automation_handler.go` 和 `migrationflow/automation_*` 在工作台集中读取真实活跃任务，每 8 秒刷新。下载百分比来自 qB 的持久化快照，其他阶段显示实际状态。版本设施、前端构建产物、两份 Compose、API 合同及发行说明同步更新。
- 152 隔离 Go 全量、Vue 类型检查、定向 ESLint、Vite 构建及原生冒烟 10 组通过。首次冒烟发现历史脚本写死 0.3.1，改为显式接收预期版本后通过。隔离应用同时返回两个进行中任务，其中模拟的 qB 真实字段为 42%，终态任务被过滤。织梦实站使用 GET 验证升序 100 条。线上首页、设置 HTML、JWT 权限和 Docker 健康均通过，浏览器渲染暂未实测。
- 152 已部署 `ptmove-nexus:1.0.5`，镜像 `sha256:32102073c575c0d5ccced0dbe5b2530fc8f6f4ece8b7f145386228a4d031b412`，保留 v1.0.4 回滚。线上自动化未配置启用，队列无活动项。压缩镜像 656431682 字节，SHA-256 为 `1143e9e774ee2db0fa810f4ff4a53bfc24e3aef38f2323486246ebb106164f57`。本记录完成时 Git 提交、双仓推送、发行附件与历史归档待执行。未实际下载、发种或访问 81。

## PTM-20261123-075 — 2026-11-23T00:45:52+08:00 — Codex root，STARPT v1.0.5 双仓发行与恢复归档

- Gitee 和 GitHub 的 v1.0.5 均指向 `b8073dbc074dd7574afb6d869f2df67ebf60e002`。GitHub 私有发行 454581652 正式发布，完整镜像、Compose、校验文件的服务端文件长度与 SHA-256 均匹配。
- Gitee 发行 1355081 提供 Compose 和校验文件，已重新下载并逐字节比对，完整镜像入口指向同版本 GitHub 私有发行。
- `docs/DEPLOYMENT.json` 已记录附件和归档证据。D 盘历史 ZIP 含标签源码、完整镜像、Compose、校验和恢复说明。核验 56 个 OCI 对象和 ZIP 内容，归档 SHA-256 为 `94c8a8592eeec144c5b49c391eed42627e4cbfd5b63b2f4c3ff13013620c5a26`，已更新项目归档索引。
- 152 健康返回 v1.0.5，发布记录独立提交，v1.0.5 标签继续指向上述发布源码提交。未执行真实下载、发种或操作 81。

## PTM-20261123-076 — 2026-11-23T08:27:29+08:00 — Codex root、preview_detail_ui、image_delta_bundle，STARPT v1.0.6 候选源站详情入口

- `scanner.go`、`source_restriction.go`、`policy.go` 和 `AutomationSettings.vue` 为候选预览和运行记录返回同源、无凭据的详情链接。已排除候选仍能查看详情，链接在新标签页打开。同步 API 合同、版本、Compose、前端产物及发行说明。
- 152 Go 全量、定向 ESLint、Vue 类型检查、Vite 构建和隔离冒烟 10 组通过。测试覆盖普通及排除候选、凭据参数、不同来源和错误 ID。线上设置页已返回通过验证的 JS 资源。未进行真实浏览器点击和站点下载。
- 152 已健康部署 `ptmove-nexus:1.0.6`，保留 1.0.5 回滚。复用 51 个经校验的 OCI 对象重建镜像附件，解压后的 Docker tar SHA-256 与 152 导出完全一致。压缩件 713803050 字节，SHA-256 `93b09b9635312eb45c169df8a18b70713a6588a2822cb345540df33dbb869e8d`。本记录编写时标签、双仓与发行待提交。未下载、发种或操作 81。

## PTM-20261123-077 — 2026-11-23T08:31:58+08:00 — Codex root，STARPT v1.0.6 双仓发行

- GitHub 和 Gitee 的 v1.0.6 标签均指向 `c2c657f5f2fa041eceb7e2143608ee7f7bfaa62e`。GitHub 私有发行 454858972 的完整镜像、Compose 和校验文件与本地 SHA-256 一致。
- Gitee 发行 1355571 的 Compose 与校验文件重新下载后逐字节匹配。完整镜像从同版本 GitHub 私有发行提供。
- D 盘历史 ZIP 已登记归档索引。包含源码、Docker 镜像、Compose、校验和恢复说明，核验 59 个 OCI 对象。ZIP SHA-256 `f9f4587ce77c7a37a68da082e88290aebca48ec86739a1c5afd69ce9a4270ff0`。
- 两站附件及 152 部署证据记入 `docs/DEPLOYMENT.json`，本记录独立提交，版本标签继续指向发布源码。未实际下载、发种或访问 81。

## PTM-20261123-078 — 2026-11-23T12:28:49+08:00 — Codex root、task_monitor_design、media_one_click、media_capture_progress、task_media_review，STARPT v1.0.7 任务进度和媒体资料补全

- `automation/scanner.go`、`migrationflow/task_monitor.go`、`automation_observation.go` 和 `HomeTaskMonitor.vue` 将候选预览、后台扫描、截图、MediaInfo、下载与发布任务接入统一状态。候选扫描显示当前条目和完成汇总，短任务保留终态，独立任务并行展示。首页与全局抽屉共享同一状态。
- `CrossSeedPanel.vue`、`mediaCompletion.ts`、`CrossSeedStepDetails.vue` 和 `refresh_flow.go` 为维护及转种草稿提供缺失资料补全。自动选 5 个不同时间点，收到 5 张互异有效图才回填。可以手动重选。已有完整媒体报告保留，优先从简介提取，再用下载器。回填只更新草稿。保留 Blu-ray 手动进入 BDInfo 的路径。
- 152 无外网隔离环境通过 Go 全量、前端定向 ESLint、Vue 类型检查、Vite 构建与 10 组原生冒烟。本机 Node 的 3 个截图选择测试也通过。认证测试验证两个独立失败任务的保留。线上 Docker 健康返回 v1.0.7 并与验证的首页文件逐字匹配。未进行真实浏览器点击、真实截图、下载、发种或 81 操作。飞牛既有客户端回传阶段和最终数量，运行中逐张计数仅本机生成路径可用。
- 部署镜像 `sha256:a99d7cc9edfe6a895e92e19dc2c7c5b56c701bb683a8921f237e782d982cd6af`，回滚镜像 v1.0.6。完整导出镜像 734015181 字节，SHA-256 `e9b2be9aad5680d6dfe56622d1af12954e302bb2e29b33df2f3d8bdc166b56b4`。附发行说明、Compose、前端构建产物和部署指标。记录时双仓提交、标签推送及发行附件上传待进行。

## PTM-20261123-079 — 2026-11-23T12:34:38+08:00 — Codex root，STARPT v1.0.7 双仓发行与补充验收

- GitHub 私有发行 454986571 的完整 152 构建镜像、Compose 和校验文件已上线，GitHub 报告的文件字节数及 SHA-256 与本地一致。Gitee 发行 1356474 的 Compose 和校验文件已重新下载校验，完整镜像沿用 GitHub 私有发行入口。两个版本标签均指向 `3793af3077373c421d1991306a96b6133bf75fd3`。
- 无外网隔离容器用合成视频实际生成 12 张候选图，任务接口观测到 0/12 至 11/12 的逐张进度及成功终态。Edge 用 152 线上资源及模拟 API 响应，在 1440 和 390 宽度验证并行任务、终态计数、共享抽屉和布局。没有调用实站或图床来完成这些验收。
- D 盘历史 ZIP 核验 62 个 OCI 对象，包含标签源码、完整镜像、Compose、校验与恢复说明。ZIP SHA-256 `3d59b0599ead7cb125c067d2cb9115586a12f30b9da579b5eaf914adf44ee1e2`，已登记归档索引。发行与测试回执写入 `docs/DEPLOYMENT.json`，该记录独立提交，标签不变。

## PTM-20261123-080 — 2026-11-23T13:06:39+08:00 — Codex root、batch_contract、batch_maintenance_ui、batch_media_persist、batch_media_review，STARPT v1.0.8 批量补齐所选维护记录

- `CrossSeedDataView.vue` 在种子维护桌面列表和手机列表提供多选和一键补全。只发送所选身份，后台逐条处理，完成后重读维护列表。首页显示当前条目、阶段以及补全、已有资料、部分补全和失败数量。
- `maintenance_media*.go` 使用现有 qB 与飞牛领单通道，从属于所选哈希的完整视频采集资料。已有可用图与媒体报告保持原样，MediaInfo 先尝试简介。截图仅收齐不同五张后落库。两个媒体字段独立保护并发修改。通过 `seedFlow.ts` 和维护保存基线防止旧表单覆盖后台成果，保留人工重选能力。
- Go 全量与前端 ESLint、Vue 类型检查及 Vite 构建通过。152 无外网镜像验证通过 10 组原生冒烟、登录控制、批次计数、所选落库和旧表单保护。补充夹具核对精准下载器选择、五张回填、三张不落库、部分结果及未选记录。Edge 在桌面和手机模拟多个种子，核对只发送两条选中记录以及完成后自动刷新。首次构建的 Go 测试夹具修正了简介样例与 qB 缓存字段，前端改用已有 Alpine 依赖重新构建通过。
- 152 已健康部署 `sha256:72ce906675fc6d2645204816c8d979934442cacda08c74b125743e2308bdeded`，保留 v1.0.7 回滚镜像。完整镜像 754253557 字节，SHA-256 `d8974cc348e7b539cd60ca09b1dee1a3ed9d137ce79973cdbcaff2246593724f`。未运行生产补全、未实际访问站点或图床、下载、发种或操作 81。记录时提交、推送和双仓发行待完成。

## PTM-20261123-081 — 2026-11-23T13:11:04+08:00 — Codex root，STARPT v1.0.8 双仓发行回执

- GitHub 私有发行 455009684 已附 152 验证过的完整 Docker 镜像、Compose 和校验文件。发布平台的大小和 SHA-256 均匹配。Gitee 发行 1356643 的 Compose、校验文件重新下载核对一致，完整镜像使用 GitHub 私有发行入口。
- GitHub 和 Gitee 标签均指向 `98ed25889e4f36c11394278b551d4880879badd7`。D 盘历史 ZIP 已登记，包含标签源码、Docker 镜像、Compose、校验与恢复说明，独立校验 65 个 OCI 对象。ZIP SHA-256 `74ae8cf8374b9f633cfc8df42de73c9df1511d496df65e7a2e9c4ac3517d6354`。
- 本条和 `docs/DEPLOYMENT.json` 为独立回执提交。152 继续运行 v1.0.8，未提交生产补全、下载、发种或操作 81。

## PTM-20261123-082 — 2026-11-23T17:57:58+08:00 — Codex root、preview_db、preview_restore_ui、preview_verify_review，STARPT v1.0.9 预览候选入库

- `automation_preview.go`、原生自动化接口和 `AutomationSettings.vue` 保存并恢复最近一份成功的网页候选。空结果保留为成功快照，失败、中断和更旧的迟到预览不替换新结果。恢复时清空勾选，不重新扫描。机器预览维持只读。
- 标注预览时间和规则差异。选中入队核对当前站点标识与公开配置摘要，以同一已核对配置创建运行记录。预览保存不消耗配额，不写运行、任务或发布队列。只保留展示字段与公开配置摘要。
- 152 Go 全量测试和前端定向 ESLint、Vue 类型检查、Vite 构建通过。隔离容器通过 10 组原生冒烟，核对空数据库、失败后保留和使用同一数据目录重建后恢复。Edge 模拟接口核对桌面与手机显示、刷新恢复、失败保留和空结果。未对生产运行预览、实际下载、发种或操作 81。
- 152 已健康部署 `sha256:d7cf67d783bf07b78171eceb2297f574930bcc20cbcebde999d66f9706463170`，回滚镜像 v1.0.8。完整镜像 774503140 字节，SHA-256 `6bd74930a232760202d55761ef20fb8d04431e82e53e04993eee41a5b0c9db85`。记录时双仓提交与发行待完成。

## PTM-20261123-083 — 2026-11-23T18:02:21+08:00 — Codex root，STARPT v1.0.9 双仓发行回执

- GitHub 私有发行 455239570 已附 152 验证过的完整镜像、Compose 和校验文件，平台校验与本地一致。Gitee 发行 1358078 已上传并重新下载核验 Compose 和校验文件，完整镜像通过 GitHub 私有发行获取。
- 两站 v1.0.9 标签同指 `d9bb190e4f2d25f1ad2ca7b0c8620f9295cde11c`。D 盘历史 ZIP 已登记项目索引，包含标签源码、Docker 镜像、Compose、校验文件及恢复说明，校验 68 个 OCI 对象。ZIP SHA-256 `62b073cb25f6b7b9659146527e39a0d6ae7ffcb89a90f21f8edfd81e7eb4df3b`。
- 152 继续健康运行 v1.0.9。发行验证记入 `docs/DEPLOYMENT.json`，独立提交，标签保持不变。未运行生产预览、下载、发种或操作 81。

## PTM-20261125-084 — 2026-11-25T12:22:31+08:00 — Codex root、site_client、plugin_ui、feiniu_proxy、tjupt_review，北洋园 RSS 与 H&R 插件

- 在现有「插件」页注册内置北洋园刷流。修改 `nexus/webui/src/views/PluginsView.vue`、新增 `components/plugins/TjuptRssPanel.vue`，并在 `internal/bootstrap/app.go`、`internal/http/handler/plugins_handler.go`、`tjupt_rss_handler.go` 和 `internal/service/plugins/catalog.go` 接入管理员路由。RSS 预览按大小筛选并显示 H&R 基础时长，默认 10 GiB。RSS 和 Token 只保留在页面内存。浏览器在安全上下文才允许输入凭据。
- `internal/service/acquire/tjupt`、`internal/service/migrationflow/tjupt_rss*.go` 和 `internal/service/downloaderclient/client.go` 读取 RSS、飞牛 qB 的 `starpt-tjupt` 标签和北洋园 H&R。预览与查询没有 qB 写操作。手动暂停默认关闭。退种时站点须重新确认当前考核达标或免考，独立签名绑定账号、种子哈希和加入时间。修改 `nexus/proxy/pull/{agent.go,hub.go,protocol.go,compose.agent.yaml,qb_tjupt.go,qb_tjupt_test.go}`，飞牛核对签名、任务、标签、完成状态及全部 Tracker passkey，仅暂停匹配任务并返回专用回执，保留文件。相关说明见 `docs/{STARPT_PLUGINS,API_CONTRACT}.md` 和 `nexus/server/docs/PROJECT_STRUCTURE.md`。
- 本地北洋园相关 Go 包和代理测试、代理 go vet、Vue 类型检查、定向 ESLint、Vite 构建与差异检查通过。Edge 桌面及 390 px 验证预览、资格按钮、拒绝后清除旧操作和非安全页面禁止输入。模拟 Hub、TLS Agent、qB 验证重复添加、未达标、错误签名、过期签名、跨账号及旧代理回执等失败路径。Windows Go 全量有既有 SQLite 文件占用清理失败。152 断网隔离容器首次编译内存不足，复用本项目缓存并降低并发后，主服务与代理 Go 全量、代理 go vet、容器健康、管理员认证、默认只读、拒绝无效预览与退种和 SPA 资源检查全部通过。
- 生产的 `ptmove-pull:0.3.1-pull.3` 已承载 qB 投种，发布分支基线缺少现网中转投种及飞牛管理界面改动。为保留既有能力，本次只在 152 验证隔离容器，未替换生产中转或主服务，无公网部署地址。待用保留现网能力的版本联调后再发布。未连接真实北洋园、飞牛 qB 或 81，未实际下载、发种、退种、提交、推送或发布。

## PTM-20261125-085 — 2026-11-25T16:31:02+08:00 — Codex root、plugin_ui、site_client、feiniu_proxy、tjupt_review，北洋园自动投种与容量控制

- `internal/service/acquire/tjupt` 适配 RSS 中携带站内下载地址的 enclosure，读取有界的 `.torrent` 元数据并校验 Tracker、v1 哈希与实际总大小，预览继续只读。更新 `internal/service/migrationflow/tjupt_rss.go`、`tjupt_automation*.go`、`internal/repository/tjupt_automation_repository*.go`、`internal/service/downloaderclient/client.go`、`internal/bootstrap/app.go` 与 `internal/http/handler/tjupt_rss_handler.go`。按可调单种体积区间和总在管容量进行去重、查 qB、持久预留和投种。默认单种 0–10 GiB、总量 50 GiB、最多 3 项、每 5 分钟检查、调度关闭。已有种子、待删及未确认预留均计入总量。qB 出现台账之外的自动任务时暂停整轮。
- 按体积累计做种并加缓冲，默认 6 小时。站点确认达标或免考后观察瞬时上传及采样间累计字节增量，默认低于 100 KiB/s 持续 60 分钟。状态未知或上传恢复时重置观察。首次满足清理条件仅标记；实际删除须独立选择，使用专用 qB 和目录，并二次核对 H&R。未确认写入保持租约与容量，签名有效期先于租约结束。只对明确未执行的投种释放预留。
- `nexus/proxy/pull/{agent.go,hub.go,protocol.go,qb_tjupt_auto*.go,qb_tjupt_delete.go,qb_tjupt_torrent.go,compose.tjupt-auto.yaml}` 增加受签名约束的加种、标记、撤销标记和删除。Agent 复核上传速度、累计做种、加入时间、账号、保存路径、文件与完整 qB 清单。只读挂载证明失败、共享或非插件任务出现时保留数据。前端 `nexus/webui/src/components/plugins/TjuptRssPanel.vue` 增加范围、总量、缓冲、速度窗口、模式和运行状态，移除原讨论中的长期保留档位。更新 `docs/{STARPT_PLUGINS,API_CONTRACT}.md` 和 `nexus/server/docs/PROJECT_STRUCTURE.md`。
- 验证。仅对用户提供的真实 RSS 进行了只读格式检查，确认 10 条同站 enclosure，未记录订阅凭据。定向容量并发、租约超时、台账缺失、时间边界、首轮仅标记和 H&R 改变拒绝删除通过。代理全量 Go、5 轮定向及 go vet 通过；Windows 符号链接用例因系统权限跳过，152 Linux 全量测试另行运行通过。Vue 类型检查、定向 ESLint、Vite 构建、Edge 桌面及 390 px 的配置保存、体积显示和取消实际删除确认通过。152 断网隔离容器核对管理员权限、默认关闭及标记模式、规则持久化、配置缺失拒绝启用、预览退种保护和 SPA 资源通过。
- 状态。生产现有中转基线仍含独立的投种能力，需在兼容该中转与专用飞牛 qB 后才能上线。152 仅隔离验证，无公网部署地址。未请求真实种子文件、未连接真实飞牛 qB 或 81，未实际投种、删除、提交、推送或发布。

## PTM-20261125-086 — 2026-11-25T16:32:18+08:00 — Codex root，152 隔离验收后的生产健康复核

- 关联 PTM-20261125-085。隔离资源均已清理。收尾时发现生产 `ptmove-nexus-ptmove-nexus-1` 于 16:31:24 自行重启。Docker 事件记录到 OOM，内核记录确认触发的是该生产容器自己的 768 MiB 内存 cgroup 限额。
- 确认仍运行 v1.0.9，Docker 健康状态为 healthy，152 本地访问 18120 的健康接口返回 HTTP 200。未执行生产重启、配置或限额修改、代码提交、推送或部署。应用内导致内存增长的路径未调查。

## PTM-20261125-087 — 2026-11-25T17:56:55+08:00 — Codex root、tjupt_review，北洋园新界面上线

- 用户明确要求上线界面。以 152 已运行的 STARPT v1.0.9 镜像为运行时，使用 PTM-20261125-085 隔离验证的前后端源码与前端构建生成 `ptmove-nexus:1.0.9-ui.1`，镜像 ID 为 `sha256:c128a6464d595ee7d550e4655064e627128e7c0d19fc077e2dba496f68ccb52e`。只替换主服务。更新 152 的 `/opt/gpt-ptmove-nexus/compose.ptmove.yaml` 默认镜像和本地 `docs/DEPLOYMENT.json`。原数据挂载、限制及 Docker 网络经比对一致，Hub 容器 ID 与启动时间核对一致。
- 切换前后确认发布队列、自动化任务和扫描计数均为零。停旧容器后生成通过 SQLite `quick_check` 的一致性快照，连同原 Compose 和回滚镜像 ID 保存在 152 项目私有目录 `releases/tjupt-ui.Dvqwer`。新版只新增北洋园表，调度关闭，清理模式为标记，北洋园写入凭据未启用。
- 152 断网构建的主服务 Go 全量及候选镜像登录、插件目录与只读模式测试通过。生产验收核对公网 `/plugins` 新入口、主机上插件构建文件与生产响应逐字节一致、未登录接口 401、35 秒后健康与队列复核通过，最终容器重启计数为零。公网健康返回 HTTP 200。
- 验收使用隔离管理员账号与生产公开入口，没有使用生产管理员凭据。真实北洋园下载、H&R、飞牛新增写入和删除未执行。现网 URL 为 `http://152.136.236.238:18120/plugins`。本次上线仅 STARPT UI 和配套 API，无 81、Git 提交、推送或双仓发行。

## PTM-20261125-TJUPT-HTTP-SYNC — 2026-11-25T21:31:51+08:00 — Codex root，北洋园插件 HTTP 管理同步

- 与 PTM-20261125-TJUPT-HTTP 同步 `nexus/webui/src/components/plugins/TjuptRssPanel.vue`，登录后可直接从现有 HTTP 管理页使用 RSS、H&R 与设置保存。更新 `docs/STARPT_PLUGINS.md`。未修改插件服务端规则。
- Vue 类型检查、插件 ESLint、Vite、Edge HTTP 浏览器交互及 152 断网原生 API 验证通过。组合部署已在 152 运行 `1.0.9-ui.3`，公网验证通过。未提交、推送、发布、执行生产种子操作或访问 81。

## PTM-20261125-TJUPT-HR-HINT-SYNC — 2026-11-25T21:39:49+08:00 — Codex root、tjupt_hr_hint_qa，北洋园体积旁显示考核时长

- 在两个工作区同步 `TjuptRssPanel.vue` 和 `TjuptHrDurationHint.vue`。RSS 选种预览的体积范围和自动投种设置的最小体积旁各有一个提示按钮。浮层显示六档基础做种时长，并说明自动清理另计缓冲。仅前端展示。
- Vue 类型检查、定向 ESLint、Vite 构建及 `git diff --check` 通过。六档体积阈值和时长与北洋园预览及自动投种后端实现核对一致。Edge 模拟接口验证桌面两处悬停、键盘 Enter，以及 390 和 320 像素手机视口各两处点按和点外关闭。各浮层完整且没有触发表单提交或写请求。
- 152 断网隔离镜像验证通过，生产更新为 `ptmove-nexus:1.0.9-ui.4`（`sha256:52ab158cdb7d816e9b8a63aa8747afbbc18712e4500214ac6f4bb4911ee005bc`），回滚镜像 `1.0.9-ui.3`。公网插件入口与 9 个入口和资源哈希核对匹配，健康正常。回执位于 `releases/tjupt-hr-hint.2q6DD7`。源码未提交、推送或双仓发布。未执行下载、发种、qB 操作或访问 81。

## PTM-20261125-TJUPT-HR-REMAINING-SYNC — 2026-11-25T22:42:00+08:00 — Codex root、tjupt_remaining_review、tjupt_remaining_browser，RSS 结果精简与真实 H&R 剩余时间

- 两个工作区的 `TjuptRssPanel.vue` 合并 RSS 考核与筛选列。达体积条件显示 `HR 48H` 等时长，超出上限显示「体积超限」，保留低于最小体积与体积未知状态。
- 将现有 H&R 查询和任务表移入「做种任务的 H&R」折叠栏。北洋园官方仓库的接口说明确认 `status.need_seed_time` 与 `statistics.seed_time`，展示以站点两者之差为准，不加本地自动清理缓冲。显示站点达标、结束、等待确认和记录不足状态。覆盖所选 qB 的现有 `starpt-tjupt` 标签查询，包括自动投种。现有查询和暂停接口保持。
- Vue 类型检查、插件 ESLint、Vite 和 `git diff --check` 通过。Edge 桌面及 390 像素手机用模拟数据验证折叠、体积边界、加载、错误重试与各考核状态。48 小时要求、36 小时站点已累计、6 小时缓冲时，显示剩余 12 小时。没有测试真实北洋园账号或真实 qB 任务。
- 152 断网 Docker 隔离验收通过，生产镜像 `ptmove-nexus:1.0.9-ui.5`（`sha256:16d43f3911fecc7f230e707aab785b1343c8632d5f65d55e1270761e051dc29e`），回滚镜像 `1.0.9-ui.4`。公网健康与 9 项入口和资源哈希核对通过，回执位于 `releases/tjupt-hr-remaining.E3qgTj`。未提交、推送、发布、下载、发种、操作 qB 或访问 81。

## PTM-20261126-OVH-TJUPT-FEED — 2026-11-26T01:03:10+08:00 — Codex root、rss_feed_core、rss_feed_qb、rss_feed_ui，北洋园插件集中配置及 RSS 循环

- 在 `nexus/webui/src/components/plugins/{TjuptRssPanel,TjuptHrDurationHint}.vue` 集中保存 RSS、H&R Token、qB、间隔、标签、分类、目录和体积规则。旧版页面凭据属于临时查询。新版凭据由服务端持久保存，刷新后显示已配置状态。页面用「保存」「试运行一次」「正式启动」「停止循环」控制，保留 RSS 预览、体积提示和实际 H&R 剩余查询。
- 接入 `nexus/server/internal/bootstrap/app.go`、`internal/http/handler/{plugins_handler,tjupt_rss_handler,tjupt_feed_handler}.go`、`internal/repository/tjupt_feed_repository.go`、`internal/service/migrationflow/tjupt_feed*.go` 及 `internal/service/downloaderclient/{client.go,tjupt_feed*.go}`。保存不启动，默认停用、间隔 1 分钟。试运行最多暂停添加 1 条。正式循环只接续身份、配置和零进度均一致的试运行任务，按订阅继续获取，使用 qB 原生接口应用标签和分类。保留去重、容量预留和写入回执保护，旧签名接口与新调度互斥。更新 `docs/{STARPT_PLUGINS,API_CONTRACT}.md` 与 `nexus/server/docs/PROJECT_STRUCTURE.md`。
- OVH 断网 Go 1.23 全量测试与 Linux 编译、隔离 Docker API 验收、Vue 类型检查、定向 ESLint、Vite 构建、差异检查通过。Edge 在 1440 和 390 像素用模拟接口核对保存、暂停添加入口、启停、标签分类、配置脱敏和 H&R 显示。原生 qB 模拟覆盖 5.3.1、4.x、重复、暂停及未知回执。最初本机抢先运行的旧测试夹具未包含新增的进度字段，合入修正夹具后的定向测试及最终 Linux 全量通过。
- 已部署 OVH `ptmove-nexus:1.0.9-ui.7`，镜像 ID `sha256:6cd22b3324641d3af179cea7574687a2d92fc4c9d107c79f16a9d1f5e95f03ed`。保留已停止的 `starpt-ui6-rollback`，数据库一致性快照和回执位于 `/project/GPT-PTMOVE/releases/rss-feed-261126`，生产数据挂载保持一致。公网 https://starpt.star1999.de/plugins 健康，5 项资源及入口匹配，匿名接口 401，OVH qB 只读连接复核通过。新版插件尚待用户保存首份 RSS 和 Token，循环为关闭。未执行生产试运行、实际下载、发种、Git 提交、推送或发行，未访问 81 或修改腾讯 152。

## PTM-20261126-OVH-TJUPT-HR-CLEANUP — 2026-11-26T01:48:45+08:00 — Codex root、tjupt_cleanup_core、tjupt_cleanup_qb、tjupt_cleanup_ui、tjupt_cleanup_review，北洋园达标后低速退种

- 原插件配置增加清理开关和两个数值，默认清理关闭、100 KiB/s、连续 60 分钟低速。旧配置可直接读取，旧 API 客户端省略新字段时保留已保存值。启用清理需保存北洋园 H&R Token。正式循环约每分钟采样 qB，RSS 仍使用设置的间隔。速率达到阈值、采样断档或进程重启时重置低速计时。
- 在 nexus/server/internal/service/migrationflow/tjupt_feed*.go 与 internal/repository/tjupt_feed_repository.go 持久化观察状态、H&R 双重确认和移除意图。仅处理已完成、正在做种且插件所有权匹配的 qB 任务。考核必须是和本次加入记录对应的同一条站点记录，两次明确达标或后达标至少相隔 15 分钟。站点错误、记录缺失和身份不符均保留。未确认的 qB 写入保留待核对，不自动重发，只在确认任务消失时释放在管额度。
- internal/service/acquire/tjupt/client.go 保留站点记录标识。downloaderclient/tjupt_feed*.go 在原生 qB 移除请求前核对全部 Tracker、身份、速率及租约，移除任务时保留下载文件。internal/platform/netproxy 为短期 qB 连接保留保护并避免在全局长期登记。同步北洋园面板、版本、Compose、发行说明、docs/API_CONTRACT.md、docs/STARPT_PLUGINS.md、docs/DEPLOYMENT.json 和服务端结构文档。
- Windows 定向 Go 测试及 go vet、OVH 断网 Go 1.23 全量与编译、Vue 类型检查、组件 ESLint、Vite 构建和差异检查通过。独立审查发现长 RSS 间隔可能把疏采样视为连续低速，已改成独立采样并补测。浏览器以 1440 和 390 像素验证默认值、凭据提示、保存、刷新恢复与原有试运行。OVH 和腾讯 152 隔离容器均验证原生 API、持久配置、认证和部署静态资源。152 首次校验脚本误触发 OVH 入口，随后修正脚本的模块入口与基础镜像清单，隔离验收通过。
- OVH 生产部署 ptmove-nexus:1.0.10-ovh.1，原 RSS 和 H&R Token 保留、RSS 与自动清理保持关闭。公网 5 项入口及资产匹配，OVH qB 只读连通，回滚容器 starpt-ui7-rollback。腾讯 152 用断网容器验证 ptmove-nexus:1.0.10，保留原生产容器。未执行生产加种、发种、退种、数据文件删除或访问 81。记录时正打包发行镜像，Git 提交、推送和发行待核对。

## PTM-20261126-TJUPT-V110-ARTIFACT — 2026-11-26T01:49:21+08:00 — Codex root，v1.0.10 发行镜像校验

- 腾讯 152 隔离验收的 linux/amd64 完整镜像已打包。镜像包含所有层，归档 794893290 字节，SHA-256 45eb975030a3dc19e5518e363f7bc03b36dd5491f73f9a6db3293240c1120693。回读校验原始 Docker tar 的 SHA-256 与 152 生成文件一致。新的发布说明、Compose 和可回滚版本已记录。OVH 生产仍运行 v1.0.10，未操作 81 或真实退种。
- 本次提交计划随标签 v1.0.10 推送至 Gitee 与 GitHub 私有仓库；提交、推送、发行与历史归档的实际结果另记回执。

## PTM-20261126-TJUPT-V110-PUBLISHED — 2026-11-26T01:52:58+08:00 — Codex root，北洋园低速退种双仓发行回执

- v1.0.10 在 Gitee 与 GitHub 私有仓库指向 1b863ab6710973307eff18ee463d028084e7c1b9。GitHub 发行 458449049 已附腾讯 152 隔离验证过的完整镜像、Compose 与校验文件，均核对字节数与 SHA-256。Gitee 发行 1368934 的 Compose、校验文件重新下载核验一致，并提供 GitHub 完整镜像入口。
- D 盘历史 ZIP 已完成，包含标签源码、完整镜像、Compose、校验和恢复说明。已核对 71 个 OCI 对象并登记归档索引。ZIP SHA-256 51a412e182bbce58846849dd8d4762ad4dd341dce05bbd7459ea989e02237f40。
- OVH 生产持续运行 v1.0.10，腾讯 152 生产容器保持原部署。本条与 docs/DEPLOYMENT.json 为独立回执提交，标签不变。未访问 81 或运行真实加种、发种和退种。
