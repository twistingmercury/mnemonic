"""Control client for the OpenAI stub: scoped failure injection and reset."""

import httpx

from mnemonic_e2e._http import HttpResponse, new_client, send


class StubClient:
    def __init__(self, base_url: str, transport: httpx.BaseTransport | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = new_client(transport)

    def _post(self, path: str, **params: str) -> None:
        response = send(self._client, "POST", self._base_url + path, params=params)
        _require_ok(path, response)

    def fail(self, match: str, n: int) -> None:
        """Make the next n embedding requests whose input contains match fail with 500."""
        self._post("/control/fail", match=match, n=str(n))

    def delay(self, match: str, ms: int) -> None:
        """Make every embedding request whose input contains match wait ms milliseconds before it is answered."""
        self._post("/control/delay", match=match, ms=str(ms))

    def remaining(self, match: str) -> int:
        """How many injected failures for match have not been served yet."""
        response = send(self._client, "GET", self._base_url + "/control/fail", params={"match": match})
        _require_ok("/control/fail", response)
        return int(response.json()["remaining"])

    def reset(self) -> None:
        """Clear every injected failure and delay."""
        self._post("/control/reset")

    def healthy(self) -> bool:
        return send(self._client, "GET", self._base_url + "/healthz").status == 200


def _require_ok(path: str, response: HttpResponse) -> None:
    if response.status >= 400:
        raise RuntimeError(f"stub {path} returned {response.status}: {response.text.strip()}")
