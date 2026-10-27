"""Administrative API and separately authenticated pull-worker protocol."""
import asyncio
from collections import defaultdict, deque
from contextlib import asynccontextmanager, suppress
from datetime import datetime, timezone
import hmac
import os
from pathlib import Path
import secrets
import time
from urllib.parse import urlsplit

from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from fastapi.staticfiles import StaticFiles

from .db import Conflict, NotFound, Store, VERSION
from .models import Checkpoint, Completion, Enqueue, Heartbeat, Lease, Login, Preview, Reconcile, Rule, RunRequest, SettingsPatch, WorkerIdentity
from .policy import evaluate_candidates
from .scheduler import tick
from .security import Sessions

MAX_PAYLOAD = 4 * 1024 * 1024


def create_app(data_dir=None, admin_password=None, secret_key=None, worker_token=None):
    """Inject configuration for isolated tests; production receives secrets via env."""
    application = FastAPI(title="PTMove", version=VERSION, docs_url=None, redoc_url=None)
    application.state.store = None
    failures = defaultdict(deque)
    config = {
        "data_dir": data_dir if data_dir is not None else os.getenv("PTMOVE_DATA_DIR", "data"),
        "password": admin_password if admin_password is not None else os.getenv("PTMOVE_ADMIN_PASSWORD"),
        "secret": secret_key if secret_key is not None else os.getenv("PTMOVE_SECRET_KEY"),
        "worker": worker_token if worker_token is not None else os.getenv("PTMOVE_WORKER_TOKEN"),
    }

    def initialize():
        if application.state.store is not None:
            return
        if os.getenv("PTMOVE_ENV") == "production" and (not config["password"] or len(config["password"]) < 12 or not config["secret"] or len(config["secret"]) < 32 or not config["worker"] or len(config["worker"]) < 32):
            raise RuntimeError("production requires admin password >=12, session secret >=32 and worker token >=32 characters")
        application.state.store = Store(Path(config["data_dir"]) / "controller.sqlite3", redaction_secrets=[config["password"], config["secret"], config["worker"]])
        application.state.sessions = Sessions(config["password"], config["secret"] or secrets.token_urlsafe(48))

    async def schedule_loop():
        while True:
            try:
                await asyncio.to_thread(tick, application.state.store)
            except Exception:
                # Do not emit exception strings: external data may contain credentials.
                with application.state.store.connection(True) as con:
                    application.state.store._event(con, "error", "定时调度失败；将在下次轮询重新检查")
            await asyncio.sleep(30)

    @asynccontextmanager
    async def lifespan(_):
        initialize()
        loop = asyncio.create_task(schedule_loop())
        try:
            yield
        finally:
            loop.cancel()
            with suppress(asyncio.CancelledError):
                await loop

    application.router.lifespan_context = lifespan

    @application.middleware("http")
    async def request_boundary(request, call_next):
        if request.method in ("POST", "PUT", "PATCH", "DELETE"):
            try:
                if int(request.headers.get("content-length", "0")) > MAX_PAYLOAD:
                    return JSONResponse({"detail": "payload too large"}, status_code=413)
            except ValueError:
                return JSONResponse({"detail": "invalid content length"}, status_code=400)
            # Bound streaming bodies as well as declared content length.
            body = bytearray()
            async for chunk in request.stream():
                body.extend(chunk)
                if len(body) > MAX_PAYLOAD:
                    return JSONResponse({"detail": "payload too large"}, status_code=413)
            request._body = bytes(body)
        response = await call_next(request)
        response.headers["X-Content-Type-Options"] = "nosniff"
        response.headers["Referrer-Policy"] = "no-referrer"
        response.headers["Cache-Control"] = "no-store"
        response.headers["Content-Security-Policy"] = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
        return response

    @application.exception_handler(Conflict)
    async def conflict(_, exc):
        return JSONResponse({"detail": str(exc)}, status_code=409)

    @application.exception_handler(NotFound)
    async def missing(_, exc):
        return JSONResponse({"detail": str(exc)}, status_code=404)

    def store():
        initialize()
        return application.state.store

    def origin_check(request):
        if request.headers.get("sec-fetch-site") == "cross-site":
            raise HTTPException(403, "cross-site request forbidden")
        origin = request.headers.get("origin")
        if origin:
            try:
                p = urlsplit(origin)
                actual = urlsplit(str(request.base_url))
                if p.scheme not in ("http", "https") or (p.scheme, p.hostname, p.port or (443 if p.scheme == "https" else 80)) != (actual.scheme, actual.hostname, actual.port or (443 if actual.scheme == "https" else 80)):
                    raise HTTPException(403, "cross-origin request forbidden")
            except ValueError:
                raise HTTPException(403, "invalid origin") from None

    def session(request):
        initialize()
        payload = application.state.sessions.read(request.cookies.get("ptmove_session", ""))
        return payload if payload and not application.state.store.session_revoked(payload["sid"]) else None

    def admin(request: Request):
        payload = session(request)
        if payload is None:
            raise HTTPException(401, "authentication required")
        if request.method not in ("GET", "HEAD", "OPTIONS"):
            origin_check(request)
            csrf = request.headers.get("x-csrf-token", "")
            if not hmac.compare_digest(csrf, payload["csrf"]):
                raise HTTPException(403, "CSRF token required")
        return payload

    def worker(request: Request):
        token = config["worker"]
        if not token:
            raise HTTPException(503, "worker token is not configured")
        if not hmac.compare_digest(request.headers.get("authorization", ""), "Bearer " + token):
            raise HTTPException(401, "worker authentication required")

    @application.get("/health")
    def health():
        return {"status": "ok", "version": VERSION}

    @application.post("/api/auth/login")
    def login(body: Login, request: Request):
        initialize()
        origin_check(request)
        if not config["password"]:
            raise HTTPException(503, "admin password is not configured")
        host = request.client.host if request.client else "unknown"
        now = time.monotonic()
        recent = failures[host]
        while recent and recent[0] < now - 300:
            recent.popleft()
        if len(recent) >= 5:
            raise HTTPException(429, "too many login attempts; retry after five minutes", headers={"Retry-After": "300"})
        if not application.state.sessions.verify_password(body.password):
            recent.append(now)
            raise HTTPException(401, "invalid password")
        recent.clear()
        cookie, payload = application.state.sessions.create()
        response = JSONResponse({"authenticated": True, "csrf_token": payload["csrf"]})
        response.set_cookie("ptmove_session", cookie, httponly=True, secure=request.url.scheme == "https" or os.getenv("PTMOVE_COOKIE_SECURE") == "1", samesite="strict", max_age=43200, path="/")
        return response

    @application.get("/api/auth/session")
    def auth_session(request: Request):
        payload = session(request)
        return {"authenticated": bool(payload), "csrf_token": payload["csrf"] if payload else None}

    @application.post("/api/auth/logout")
    def logout(payload=Depends(admin), db=Depends(store)):
        db.revoke_session(payload["sid"], payload["exp"])
        response = JSONResponse({"authenticated": False})
        response.delete_cookie("ptmove_session", path="/")
        return response

    @application.get("/api/dashboard", dependencies=[Depends(admin)])
    def dashboard(db=Depends(store)):
        return db.dashboard()

    @application.get("/api/rules", dependencies=[Depends(admin)])
    def rules(db=Depends(store)):
        return db.get_rule()

    @application.put("/api/rules", dependencies=[Depends(admin)])
    def put_rules(body: Rule, db=Depends(store)):
        return db.set_rule(body.model_dump())

    @application.get("/api/settings", dependencies=[Depends(admin)])
    def settings(db=Depends(store)):
        return db.get_settings()

    @application.put("/api/settings", dependencies=[Depends(admin)])
    def put_settings(body: SettingsPatch, db=Depends(store)):
        try:
            return db.set_settings(body.model_dump(exclude_unset=True, exclude_none=True))
        except ValueError as exc:
            raise HTTPException(422, str(exc)) from None

    @application.get("/api/candidates", dependencies=[Depends(admin)])
    def candidates(db=Depends(store)):
        return db.candidates()

    @application.post("/api/preview", dependencies=[Depends(admin)])
    def preview(body: Preview, db=Depends(store)):
        if body.demo:
            items = []
            for index, title in enumerate(("示例电影 · 官方高保真版", "示例电影 · 小体积收藏版", "示例电影 · 禁止转载", "示例电影 · 重复检查未知", "示例剧集 S01E03"), 1):
                items.append({"id": f"demo-{index}", "source_id": f"demo-{index}", "title": title, "size_bytes": (index + 1) * 1024 ** 3, "seeders": 18 - index, "leechers": index, "official": index == 1, "category": "movie", "detail_url": f"https://zmpt.cc/details.php?id=demo-{index}", "download_url": f"https://zmpt.cc/download.php?id=demo-{index}", "restricted": index == 3, "restriction_reason": "示例：禁止转载" if index == 3 else "", "permission_verified": True, "target_exists": None if index == 4 else False, "target_work_exists": False, "release_group": "DEMO", "published_at": datetime.now(timezone.utc).isoformat(), "metadata": {"description": "仅在内存中展示，不连接站点、不消耗配额。"}})
            return {"items": evaluate_candidates(items, db.get_rule()), "demo": True}
        return db.create_run(preview=True)

    @application.post("/api/runs", dependencies=[Depends(admin)])
    def run(body: RunRequest, db=Depends(store)):
        return db.create_run()

    @application.get("/api/tasks", dependencies=[Depends(admin)])
    def tasks(db=Depends(store)):
        return db.tasks()

    @application.post("/api/tasks", dependencies=[Depends(admin)])
    def enqueue(body: Enqueue, db=Depends(store)):
        return db.enqueue(body.candidate_ids)

    @application.post("/api/tasks/{task_id}/cancel", dependencies=[Depends(admin)])
    def cancel(task_id: int, db=Depends(store)):
        return db.cancel(task_id)

    @application.post("/api/tasks/{task_id}/retry", dependencies=[Depends(admin)])
    def retry(task_id: int, db=Depends(store)):
        return db.retry(task_id)

    @application.post("/api/tasks/{task_id}/reconcile", dependencies=[Depends(admin)])
    def reconcile(task_id: int, body: Reconcile, db=Depends(store)):
        try:
            return db.reconcile(task_id, body.published_url)
        except ValueError as exc:
            if isinstance(exc, (Conflict, NotFound)):
                raise
            raise HTTPException(422, str(exc)) from None

    @application.get("/api/events", dependencies=[Depends(admin)])
    def events(db=Depends(store)):
        return db.events()

    @application.post("/api/worker-token", dependencies=[Depends(admin)])
    def get_worker_token():
        if not config["worker"]:
            raise HTTPException(503, "worker token is not configured")
        return {"token": config["worker"]}

    @application.get("/api/worker-compose", dependencies=[Depends(admin)])
    def compose():
        return {"compose": """name: gpt-ptmove-nas
services:
  worker:
    image: ptmove:0.1.1
    build: .
    command: [python, -m, ptmove.worker]
    restart: unless-stopped
    network_mode: host
    env_file: .env.nas
    environment:
      PTMOVE_WORKER_STATE_DIR: /state
      TZ: Asia/Shanghai
    volumes:
      - ./worker-state:/state
    healthcheck:
      disable: true
    read_only: true
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    pids_limit: 96
    mem_limit: 256m
    logging:
      driver: json-file
      options:
        max-size: 5m
        max-file: "3"
"""}

    @application.post("/api/worker/heartbeat", dependencies=[Depends(worker)])
    def heartbeat(body: Heartbeat, db=Depends(store)):
        return db.heartbeat(body.worker_id, body.version, body.capabilities)

    @application.get("/api/worker/config", dependencies=[Depends(worker)])
    def worker_config(db=Depends(store)):
        return {"rule": db.get_rule(), "settings": db.get_settings(full=True), "version": VERSION}

    @application.post("/api/worker/claim", dependencies=[Depends(worker)])
    def claim(body: WorkerIdentity, db=Depends(store)):
        return db.claim(body.worker_id)

    @application.post("/api/worker/jobs/{job_id}/renew", dependencies=[Depends(worker)])
    def renew(job_id: int, body: Lease, db=Depends(store)):
        return db.renew(job_id, body.worker_id, body.lease_token)

    @application.post("/api/worker/jobs/{job_id}/checkpoint", dependencies=[Depends(worker)])
    def checkpoint(job_id: int, body: Checkpoint, db=Depends(store)):
        return db.checkpoint(job_id, body.worker_id, body.lease_token, body.model_dump(exclude_unset=True, exclude={"worker_id", "lease_token"}))

    @application.post("/api/worker/jobs/{job_id}/complete", dependencies=[Depends(worker)])
    def complete(job_id: int, body: Completion, db=Depends(store)):
        try:
            return db.complete(job_id, body.worker_id, body.lease_token, body.success, body.result)
        except ValueError as exc:
            if isinstance(exc, (Conflict, NotFound)):
                raise
            raise HTTPException(422, str(exc)) from None

    static = Path(__file__).parent / "static"
    if static.is_dir():
        application.mount("/", StaticFiles(directory=static, html=True), name="static")
    return application


app = create_app()
