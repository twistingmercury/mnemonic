"""Typed client for the Mnemonic REST API (and the /health and /version of any service)."""

from dataclasses import dataclass
from typing import Any

import httpx

from mnemonic_e2e._http import HttpResponse, new_client, send

_PATTERNS = "/v1/api/patterns"
_DEFAULT_FIELDS: dict[str, Any] = {
    "tags": ["integration-test"],
    "entity_type": "go-pattern",
    "language": "go",
    "domain": "backend",
}


@dataclass(frozen=True)
class Pattern:
    id: str
    name: str
    enrichment_status: str = ""
    enrichment_error: str | None = None
    enriched_at: str | None = None


@dataclass(frozen=True)
class ChunkSummary:
    chunk_index: int
    section_title: str
    enrichment_status: str


def pattern_payload(name: str, content: str, **overrides: Any) -> dict[str, Any]:
    """Build a valid create/update body; an override of None drops that field."""
    fields = {"name": name, "content": content, **_DEFAULT_FIELDS, **overrides}
    return {key: value for key, value in fields.items() if value is not None}


class ApiResponse(HttpResponse):
    """An HTTP response with typed readers for the API's resource shapes."""

    def pattern(self) -> Pattern:
        data = self.json()
        return Pattern(
            id=data["id"],
            name=data["name"],
            enrichment_status=data.get("enrichment_status", ""),
            enrichment_error=data.get("enrichment_error"),
            enriched_at=data.get("enriched_at"),
        )

    def patterns(self) -> list[Pattern]:
        """The patterns of a list response."""
        return [
            Pattern(
                id=item["id"],
                name=item["name"],
                enrichment_status=item.get("enrichment_status", ""),
            )
            for item in self.json()["data"]
        ]

    def pattern_ids(self) -> list[str]:
        """The pattern ids of a search response, best match first."""
        return [item["pattern_id"] for item in self.json()["results"]]

    def chunk_list(self) -> list[ChunkSummary]:
        return [
            ChunkSummary(c["chunk_index"], c["section_title"], c["enrichment_status"]) for c in self.json()["chunks"]
        ]


class ApiClient:
    def __init__(self, base_url: str, transport: httpx.BaseTransport | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = new_client(transport)

    def _request(self, method: str, path: str, **kwargs: Any) -> ApiResponse:
        response = send(self._client, method, self._base_url + path, **kwargs)
        return ApiResponse(response.status, response.headers, response.body)

    def create(self, payload: dict[str, Any]) -> ApiResponse:
        return self._request("POST", _PATTERNS, json=payload)

    def get(self, pattern_id: str) -> ApiResponse:
        return self._request("GET", f"{_PATTERNS}/{pattern_id}")

    def update(self, pattern_id: str, payload: dict[str, Any]) -> ApiResponse:
        return self._request("PUT", f"{_PATTERNS}/{pattern_id}", json=payload)

    def delete(self, pattern_id: str) -> ApiResponse:
        return self._request("DELETE", f"{_PATTERNS}/{pattern_id}")

    def list_patterns(self, **params: Any) -> ApiResponse:
        return self._request("GET", _PATTERNS, params=params)

    def search(self, q: str, tags: list[str] | None = None, **params: Any) -> ApiResponse:
        query = {"q": q, **params}
        if tags:
            query["tags"] = ",".join(tags)

        return self._request("GET", f"{_PATTERNS}/search", params=query)

    def chunks(self, pattern_id: str) -> ApiResponse:
        return self._request("GET", f"{_PATTERNS}/{pattern_id}/chunks")

    def get_path(self, path: str) -> ApiResponse:
        """GET any path, such as /health or /version on the service behind base_url."""
        return self._request("GET", path)
