"""Validated public inputs. Limits are safety ceilings, not just UI hints."""
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator


class Rule(BaseModel):
    model_config = ConfigDict(extra="forbid")
    enabled: bool = False
    day: int = Field(1, ge=1, le=31)
    hour: int = Field(2, ge=0, le=23)
    minute: int = Field(0, ge=0, le=59)
    monthly_limit: int = Field(6, ge=1, le=100)
    min_size_gib: float = Field(1, ge=1, le=1024)
    max_size_gib: float = Field(10, ge=1, le=1024)
    monthly_budget_gib: float = Field(60, ge=1, le=10240)
    min_seeders: int = Field(3, ge=0, le=1000000)
    min_leechers: int = Field(0, ge=0, le=1000000)
    candidate_limit: int = Field(50, ge=1, le=500)
    official_policy: Literal["only", "prefer", "exclude"] = "prefer"
    categories: list[Literal["movie", "tv", "other"]] = Field(default_factory=lambda: ["movie"], min_length=1, max_length=3)
    publish_interval_seconds: int = Field(600, ge=60, le=86400)
    max_download_hours: int = Field(24, ge=1, le=168)
    max_run_hours: int = Field(72, ge=1, le=336)
    source_max_age_days: int = Field(30, ge=1, le=3650)

    @model_validator(mode="after")
    def coherent(self):
        if self.min_size_gib > self.max_size_gib:
            raise ValueError("minimum size exceeds maximum size")
        if self.max_download_hours > self.max_run_hours:
            raise ValueError("download timeout exceeds run timeout")
        return self


class Login(BaseModel):
    password: str = Field(max_length=1024)


class SettingsPatch(BaseModel):
    model_config = ConfigDict(extra="forbid")
    source_cookie: str | None = Field(None, max_length=16384)
    target_cookie: str | None = Field(None, max_length=16384)
    source_rss_url: str | None = Field(None, max_length=4096)
    qb_url: str | None = Field(None, max_length=4096)
    qb_username: str | None = Field(None, max_length=256)
    qb_password: str | None = Field(None, max_length=4096)
    qb_api_key: str | None = Field(None, max_length=4096)
    qb_save_path: str | None = Field(None, max_length=4096)
    media_root: str | None = Field(None, max_length=4096)
    ptnexus_url: str | None = Field(None, max_length=4096)
    ptnexus_username: str | None = Field(None, max_length=256)
    ptnexus_downloader_id: str | None = Field(None, max_length=256)
    ptnexus_password: str | None = Field(None, max_length=4096)


class Preview(BaseModel):
    demo: bool = False


class RunRequest(BaseModel):
    mode: Literal["manual"] = "manual"


class Enqueue(BaseModel):
    candidate_ids: list[int] = Field(min_length=1, max_length=500)


class Reconcile(BaseModel):
    model_config = ConfigDict(extra="forbid")
    published_url: str = Field(min_length=1, max_length=4096)


class WorkerIdentity(BaseModel):
    worker_id: str = Field(min_length=1, max_length=128, pattern=r"^[\w.:-]+$")


class Heartbeat(WorkerIdentity):
    version: str = Field("unknown", max_length=100)
    capabilities: dict = Field(default_factory=dict)


class Lease(WorkerIdentity):
    lease_token: str = Field(min_length=16, max_length=256)


class Checkpoint(Lease):
    status: Literal["leased", "downloading", "preparing", "publishing", "published", "seeding", "succeeded", "failed", "needs_review"]
    message: str = Field("", max_length=4096)
    published_url: str | None = Field(None, max_length=4096)
    source_hash: str | None = Field(None, max_length=128)
    target_hash: str | None = Field(None, max_length=128)
    downloaded_bytes: int | None = Field(None, ge=0)
    publish_attempted: bool | None = None


class Completion(Lease):
    success: bool
    result: dict = Field(default_factory=dict)
