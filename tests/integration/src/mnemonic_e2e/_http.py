"""Shared HTTP plumbing: one response type and errors that name the URL."""

import json
from dataclasses import dataclass
from typing import Any

import httpx

_TIMEOUT_S = 10.0


@dataclass(frozen=True)
class HttpResponse:
    status: int
    headers: dict[str, str]
    body: bytes

    @property
    def text(self) -> str:
        return self.body.decode(errors="replace")

    def json(self) -> Any:
        return json.loads(self.body)


def new_client(transport: httpx.BaseTransport | None) -> httpx.Client:
    return httpx.Client(transport=transport, timeout=_TIMEOUT_S)


def send(client: httpx.Client, method: str, url: str, **kwargs: Any) -> HttpResponse:
    """Send a request; an unreachable or slow service raises ConnectionError naming url."""
    try:
        response = client.request(method, url, **kwargs)
    except httpx.TransportError as exc:
        raise ConnectionError(f"{method} {url}: {exc}") from exc

    return HttpResponse(response.status_code, dict(response.headers), response.content)
