"""Pure selection policy: uncertainty always rejects a candidate."""
from datetime import datetime, timezone
import math
import re
from urllib.parse import urlsplit

from .models import Rule

GIB = 1024 ** 3
CANDIDATE_FIELDS = {"id", "source_id", "title", "size_bytes", "seeders", "leechers", "official", "category", "detail_url", "download_url", "restricted", "restriction_reason", "permission_verified", "target_exists", "target_work_exists", "info_hash", "release_group", "published_at", "selected", "reason", "rank", "metadata"}


def _number(value, default=-1):
    try:
        result = float(value)
        return result if math.isfinite(result) else default
    except (TypeError, ValueError):
        return default


def _source_url(value):
    try:
        parsed = urlsplit(str(value or ""))
        return parsed.scheme == "https" and parsed.hostname == "zmpt.cc" and parsed.username is None and parsed.port in (None, 443)
    except ValueError:
        return False


def evaluate_candidates(items, rule):
    rule = Rule.model_validate(rule).model_dump() if not isinstance(rule, Rule) else rule.model_dump()
    evaluated = []
    now = datetime.now(timezone.utc)
    for raw in items[:2000]:
        if not isinstance(raw, dict):
            continue
        item = {key: value for key, value in raw.items() if key in CANDIDATE_FIELDS}
        item["metadata"] = raw.get("metadata") if isinstance(raw.get("metadata"), dict) else {}
        reasons = []
        size, seeders, leechers = (_number(raw.get(k)) for k in ("size_bytes", "seeders", "leechers"))
        if raw.get("permission_verified") is not True:
            reasons.append("转载权限未确认")
        if raw.get("restricted") is not False:
            reasons.append(str(raw.get("restriction_reason") or "禁止转载或限制状态未知"))
        if raw.get("target_exists") is not False or raw.get("target_work_exists") not in (True, False) or not isinstance(raw.get("target_work_exists"), bool):
            reasons.append("目标站重复检查未通过或结果未知")
        if not rule["min_size_gib"] * GIB <= size <= rule["max_size_gib"] * GIB:
            reasons.append("体积不在规则范围")
        if seeders < rule["min_seeders"] or leechers < rule["min_leechers"]:
            reasons.append("做种或下载人数不足")
        if raw.get("category") not in rule["categories"]:
            reasons.append("分类不匹配")
        title = str(raw.get("title") or "")[:1024]
        if item["metadata"].get("episode") or re.search(r"\bS\d{1,2}E\d{1,3}\b|\bEP?\d{1,3}\b|第\s*\d+\s*[集话話]", title, re.I):
            reasons.append("不处理单集资源")
        if rule["official_policy"] == "only" and raw.get("official") is not True:
            reasons.append("仅限官种")
        if rule["official_policy"] == "exclude" and raw.get("official") is not False:
            reasons.append("排除官种")
        if not _source_url(raw.get("detail_url")) or not _source_url(raw.get("download_url")):
            reasons.append("来源链接不可信或缺失")
        published_at = raw.get("published_at")
        if published_at:
            try:
                stamp = datetime.fromisoformat(str(published_at).replace("Z", "+00:00"))
                if stamp.tzinfo is None:
                    stamp = stamp.replace(tzinfo=timezone.utc)
                age = (now - stamp).total_seconds()
                if age < -86400 or age > rule["source_max_age_days"] * 86400:
                    reasons.append("资源发布时间不在规则范围")
            except (ValueError, TypeError):
                reasons.append("发布时间无效")
        else:
            reasons.append("发布时间未知")
        item.update(title=title, selected=not reasons, reason="；".join(reasons) if reasons else "符合规则", size_bytes=max(0, int(size)), seeders=max(0, int(seeders)), leechers=max(0, int(leechers)))
        evaluated.append(item)
    evaluated.sort(key=lambda c: (not c["selected"], c.get("target_work_exists") is True, not (rule["official_policy"] == "prefer" and c.get("official") is True), -c["seeders"], -c["leechers"], c["size_bytes"], str(c.get("source_id", ""))))
    selected = 0
    for rank, item in enumerate(evaluated, 1):
        if item["selected"]:
            selected += 1
            if selected > rule["candidate_limit"]:
                item.update(selected=False, reason="超出候选数量限制")
        item["rank"] = rank
    return evaluated
