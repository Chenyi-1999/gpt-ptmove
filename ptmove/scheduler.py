"""Shanghai calendar scheduling. Database unique keys arbitrate concurrent ticks."""
from datetime import datetime, timedelta, timezone
from calendar import monthrange

SHANGHAI = timezone(timedelta(hours=8), "Asia/Shanghai")


def local_now(now=None):
    return (now or datetime.now(timezone.utc)).astimezone(SHANGHAI)


def month_key(now=None):
    return local_now(now).strftime("%Y-%m")


def due(rule, now=None):
    current = local_now(now)
    scheduled = current.replace(day=min(rule["day"], monthrange(current.year, current.month)[1]), hour=rule["hour"], minute=rule["minute"], second=0, microsecond=0)
    return rule["enabled"] and current >= scheduled


def next_run(rule, now=None):
    if not rule["enabled"]:
        return None
    current = local_now(now)
    stamp = current.replace(day=min(rule["day"], monthrange(current.year, current.month)[1]), hour=rule["hour"], minute=rule["minute"], second=0, microsecond=0)
    if stamp <= current:
        following = stamp.replace(day=28) + timedelta(days=4)
        stamp = following.replace(day=min(rule["day"], monthrange(following.year, following.month)[1]))
    return stamp.isoformat()


def tick(store, now=None):
    rule = store.get_rule()
    if due(rule, now):
        return store.create_run(scheduled=True, now=now)
    return None
