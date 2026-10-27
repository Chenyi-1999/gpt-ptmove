from datetime import datetime, timedelta, timezone

import pytest
from pydantic import ValidationError

from ptmove.models import Rule
from ptmove.policy import evaluate_candidates


def candidate(source_id="1", **changes):
    return {"source_id": source_id, "title": f"Movie {source_id}", "size_bytes": 2 * 1024 ** 3, "seeders": 10, "leechers": 2, "official": False, "category": "movie", "detail_url": f"https://zmpt.cc/details.php?id={source_id}", "download_url": f"https://zmpt.cc/download.php?id={source_id}", "restricted": False, "restriction_reason": "", "permission_verified": True, "target_exists": False, "target_work_exists": False, "release_group": "GROUP", "published_at": datetime.now(timezone.utc).isoformat(), "metadata": {}, **changes}


def test_ranking_prefers_missing_work_then_official_then_seeders():
    items = [candidate("fallback", official=True, target_work_exists=True, seeders=100), candidate("regular", seeders=30), candidate("official", official=True, seeders=5)]
    result = evaluate_candidates(items, Rule())
    assert [item["source_id"] for item in result] == ["official", "regular", "fallback"]
    assert all(item["selected"] for item in result)
    assert [item["rank"] for item in result] == [1, 2, 3]


@pytest.mark.parametrize("change", [
    {"permission_verified": None}, {"permission_verified": 1}, {"restricted": None}, {"restricted": True},
    {"target_exists": None}, {"target_exists": True}, {"target_exists": 0}, {"target_work_exists": None}, {"target_work_exists": 0},
    {"size_bytes": float("nan")}, {"size_bytes": 1024 ** 3 - 1}, {"size_bytes": 10 * 1024 ** 3 + 1},
    {"seeders": 2}, {"title": "Series S01E03"}, {"metadata": {"episode": True}}, {"category": "tv"},
    {"download_url": "https://81.70.24.5/download"}, {"detail_url": "https://zmpt.cc.evil.test/x"},
    {"published_at": None}, {"published_at": "invalid"}, {"published_at": (datetime.now(timezone.utc) - timedelta(days=31)).isoformat()},
])
def test_uncertainty_and_hard_limits_fail_closed(change):
    assert evaluate_candidates([candidate(**change)], Rule())[0]["selected"] is False


def test_official_filter_and_candidate_limit():
    rule = Rule(official_policy="only", candidate_limit=1)
    result = evaluate_candidates([candidate("a", official=True), candidate("b", official=True), candidate("c")], rule)
    assert sum(item["selected"] for item in result) == 1
    assert result[2]["selected"] is False
    assert evaluate_candidates([candidate(official=True)], Rule(official_policy="exclude"))[0]["selected"] is False


@pytest.mark.parametrize("changes", [{"monthly_limit": 101}, {"min_seeders": -1}, {"max_size_gib": 1025}, {"min_size_gib": 0}, {"monthly_budget_gib": 10241}, {"min_size_gib": 4, "max_size_gib": 2}, {"max_download_hours": 80, "max_run_hours": 10}])
def test_rule_safety_ceilings(changes):
    with pytest.raises(ValidationError):
        Rule(**changes)


def test_defaults_are_editable():
    rule = Rule(monthly_limit=15, max_size_gib=30, monthly_budget_gib=200, min_seeders=0, day=31)
    assert rule.monthly_limit == 15 and rule.max_size_gib == 30
