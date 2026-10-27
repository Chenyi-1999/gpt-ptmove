from datetime import datetime, timezone

from ptmove.db import Store
from ptmove.models import Rule
from ptmove.scheduler import tick


def test_dashboard_shows_due_catch_up_then_next_month(tmp_path):
    now = datetime(2026, 10, 27, 4, tzinfo=timezone.utc)
    store = Store(tmp_path / "controller.sqlite3", clock=lambda: now.timestamp())
    store.set_rule(Rule(enabled=True).model_dump())
    assert datetime.fromisoformat(store.dashboard()["next_run_at"]) == now
    assert tick(store, now)
    assert store.dashboard()["next_run_at"].startswith("2026-11-01T02:00")
