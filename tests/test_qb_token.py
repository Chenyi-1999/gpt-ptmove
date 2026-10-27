import json
import sqlite3

import httpx
import pytest

from ptmove.adapters import AdapterError, QBittorrent
from ptmove.db import Store
from ptmove.models import SettingsPatch
from ptmove.security import SECRET_SETTINGS, SETTING_KEYS


TOKEN = "qbt_" + "A" * 28


def test_qb_api_token_authenticates_version_get_and_later_get_post_without_login():
    calls = []

    def handler(request):
        calls.append((request.method, request.url.path, request.headers.get("Authorization")))
        if request.url.path.endswith("app/version"):
            return httpx.Response(200, text="v5.2.0")
        if request.url.path.endswith("app/webapiVersion"):
            return httpx.Response(200, text="2.14.1")
        if request.url.path.endswith("torrents/info"):
            return httpx.Response(200, json=[])
        return httpx.Response(200)

    client = httpx.Client(base_url="http://qb:8080", transport=httpx.MockTransport(handler))
    qb = QBittorrent("http://qb:8080", "ignored-user", "ignored-password", client, api_key=TOKEN)
    assert qb.login() == {"version": "v5.2.0", "api_version": "2.14.1", "auth_method": "api_key"}
    assert qb.info("a" * 40) is None
    qb.start("a" * 40)

    assert [path for _, path, _ in calls] == [
        "/api/v2/app/version", "/api/v2/app/webapiVersion",
        "/api/v2/torrents/info", "/api/v2/torrents/start",
    ]
    assert all(method in {"GET", "POST"} and auth == "Bearer " + TOKEN for method, _, auth in calls)
    assert "auth/login" not in repr(calls)
    assert TOKEN not in repr(qb)


@pytest.mark.parametrize("token", ["qbt_" + "A" * 27, "qbt_" + "A" * 27 + "!", "password"])
def test_qb_api_token_rejects_invalid_format_without_echo(token):
    with pytest.raises(AdapterError, match="Token 格式无效") as error:
        QBittorrent("http://qb:8080", "", "", api_key=token)
    assert token not in str(error.value)


@pytest.mark.parametrize("version,api", [("v5.1.9", "2.14.1"), ("v5.2.0", "2.14.0")])
def test_qb_api_token_requires_supported_versions_without_password_fallback(version, api):
    calls = []

    def handler(request):
        calls.append(request.url.path)
        if request.url.path.endswith("app/version"):
            return httpx.Response(200, text=version)
        return httpx.Response(200, text=api)

    qb = QBittorrent("http://qb:8080", "user", "password", httpx.Client(
        base_url="http://qb:8080", transport=httpx.MockTransport(handler)), api_key=TOKEN)
    with pytest.raises(AdapterError, match="qB API Token 需要") as error:
        qb.login()
    assert calls == ["/api/v2/app/version", "/api/v2/app/webapiVersion"]
    assert "auth/login" not in calls
    assert TOKEN not in str(error.value)


def test_qb_api_token_transport_failure_does_not_chain_request_with_authorization_header():
    def handler(request):
        raise httpx.ReadTimeout("transport failed", request=request)

    qb = QBittorrent("http://qb:8080", "", "", httpx.Client(
        base_url="http://qb:8080", transport=httpx.MockTransport(handler)), api_key=TOKEN)
    with pytest.raises(AdapterError) as error:
        qb.login()
    assert TOKEN not in str(error.value)
    assert error.value.__cause__ is None
    assert error.value.__context__ is None


def test_qb_api_token_setting_is_encrypted_secret_and_redacted(tmp_path):
    store = Store(tmp_path / "controller.sqlite3")
    assert "qb_api_key" in SECRET_SETTINGS and "qb_api_key" in SETTING_KEYS
    assert SettingsPatch(qb_api_key=TOKEN).qb_api_key == TOKEN

    public = store.set_settings({"qb_api_key": TOKEN})
    assert public["qb_api_key"] == ""
    assert public["configured_secrets"]["qb_api_key"] is True
    assert store.get_settings(full=True)["qb_api_key"] == TOKEN

    with sqlite3.connect(store.path) as connection:
        stored = connection.execute("SELECT value FROM config WHERE key='settings'").fetchone()[0]
    assert TOKEN not in stored
    assert TOKEN not in json.dumps(public)
