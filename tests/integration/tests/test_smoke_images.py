"""Smoke checks that only the running images can answer: stamped build info, metrics, swagger, port boundary.

These need the running compose stack. They are read-only, so they share one stack in any order.
"""

import socket
from urllib.parse import urlparse

import pytest

from mnemonic_e2e.api import ApiClient
from mnemonic_e2e.config import Settings

# An unstamped binary reports this for version, build_date and commit.
UNSTAMPED = "n/a"
CONNECT_TIMEOUT_S = 5.0

SERVICES = pytest.mark.parametrize("service", ["api", "mcp", "enricher"])


def ops_url(settings: Settings, service: str) -> str:
    """The base URL of the service's /health and /version listener (the MCP service keeps MCP on another port)."""
    return {"api": settings.api_url, "mcp": settings.mcp_ops_url, "enricher": settings.enricher_url}[service]


def host_of(url: str) -> str:
    host = urlparse(url).hostname
    assert host, f"no host in {url!r}"

    return host


@SERVICES
def test_version_is_stamped(settings: Settings, service: str) -> None:
    client = ApiClient(ops_url(settings, service))
    want_name = f"mnemonic-{service}"

    version = client.get_path("/version")
    assert version.status == 200, f"/version: status {version.status}\nbody: {version.text}"
    body = version.json()
    assert body["service"] == want_name
    for field in ("version", "build_date", "commit"):
        assert body[field] not in ("", UNSTAMPED), f"/version {field} is {body[field]!r}: the binary is not stamped"

    health = client.get_path("/health")
    assert health.status == 200, f"/health: status {health.status}\nbody: {health.text}"
    assert health.json()["name"] == want_name


@SERVICES
def test_metrics_endpoint(settings: Settings, service: str) -> None:
    host = host_of(ops_url(settings, service))
    metrics = ApiClient(f"http://{host}:{settings.metrics_port}").get_path("/metrics")

    assert metrics.status == 200, f"{host} /metrics: status {metrics.status}\nbody: {metrics.text}"
    content_type = metrics.headers.get("content-type", "")
    assert content_type.startswith(("text/plain", "application/openmetrics-text")), content_type
    for series in ("go_goroutines", "go_memstats_alloc_bytes"):
        assert series in metrics.text, f"{host} /metrics has no {series}"


def test_api_swagger_served(settings: Settings) -> None:
    swagger = ApiClient(settings.api_url).get_path("/swagger/index.html")

    assert swagger.status == 200, f"/swagger/index.html: status {swagger.status}\nbody: {swagger.text}"


def test_api_does_not_serve_mcp(settings: Settings) -> None:
    mcp = ApiClient(settings.api_url).get_path("/mcp")
    assert mcp.status == 404, f"api GET /mcp: status {mcp.status}\nbody: {mcp.text}"

    mcp_port = urlparse(settings.mcp_url).port
    assert mcp_port, f"no port in {settings.mcp_url!r}"
    with pytest.raises(ConnectionRefusedError):
        socket.create_connection((host_of(settings.api_url), mcp_port), timeout=CONNECT_TIMEOUT_S).close()
