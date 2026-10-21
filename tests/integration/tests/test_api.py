import json

import httpx

from mnemonic_e2e.api import ApiClient, pattern_payload


def client_with(handler: httpx.MockTransport) -> ApiClient:
    return ApiClient("http://api.test:8080", transport=handler)


def test_pattern_payload_has_the_defaults_the_api_requires() -> None:
    payload = pattern_payload("my-pattern", "body")

    assert payload == {
        "name": "my-pattern",
        "content": "body",
        "tags": ["integration-test"],
        "entity_type": "go-pattern",
        "language": "go",
        "domain": "backend",
    }


def test_pattern_payload_overrides_win_and_none_removes_a_field() -> None:
    payload = pattern_payload("n", "c", language=None, domain="ops")

    assert "language" not in payload
    assert payload["domain"] == "ops"


def test_create_posts_the_payload_and_types_the_pattern() -> None:
    seen: list[httpx.Request] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(
            202,
            json={"id": "abc", "name": "n", "enrichment_status": "pending", "extra": 1},
        )

    response = client_with(httpx.MockTransport(handle)).create(pattern_payload("n", "c"))

    assert response.status == 202
    pattern = response.pattern()
    assert (pattern.id, pattern.name, pattern.enrichment_status) == (
        "abc",
        "n",
        "pending",
    )
    assert seen[0].method == "POST"
    assert str(seen[0].url) == "http://api.test:8080/v1/api/patterns"
    assert json.loads(seen[0].content)["name"] == "n"


def test_get_update_delete_and_chunks_use_the_pattern_paths() -> None:
    seen: list[tuple[str, str]] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append((request.method, request.url.path))
        return httpx.Response(204)

    api = client_with(httpx.MockTransport(handle))
    api.get("p1")
    api.update("p1", {"name": "n"})
    api.delete("p1")
    api.chunks("p1")

    assert seen == [
        ("GET", "/v1/api/patterns/p1"),
        ("PUT", "/v1/api/patterns/p1"),
        ("DELETE", "/v1/api/patterns/p1"),
        ("GET", "/v1/api/patterns/p1/chunks"),
    ]


def test_search_sends_query_and_threshold_and_types_the_results() -> None:
    def handle(request: httpx.Request) -> httpx.Response:
        assert request.url.params["q"] == "tokabc"
        assert request.url.params["threshold"] == "0.1"
        assert request.url.params["tags"] == "a,b"
        return httpx.Response(
            200,
            json={
                "results": [{"pattern_id": "p1", "similarity": 0.4}],
                "query": "tokabc",
            },
        )

    response = client_with(httpx.MockTransport(handle)).search("tokabc", threshold=0.1, tags=["a", "b"])

    assert response.pattern_ids() == ["p1"]


def test_chunks_response_is_typed() -> None:
    body = {
        "chunks": [{"chunk_index": 0, "section_title": "Core", "enrichment_status": "enriched"}],
        "count": 1,
    }
    api = client_with(httpx.MockTransport(lambda _: httpx.Response(200, json=body)))

    chunks = api.chunks("p1").chunk_list()

    assert [(c.chunk_index, c.section_title, c.enrichment_status) for c in chunks] == [(0, "Core", "enriched")]


def test_list_forwards_filters_and_types_the_page() -> None:
    def handle(request: httpx.Request) -> httpx.Response:
        assert request.url.params["limit"] == "5"
        assert request.url.params["search"] == "x"
        return httpx.Response(
            200,
            json={"data": [{"id": "p1", "name": "a", "enrichment_status": "pending"}]},
        )

    page = client_with(httpx.MockTransport(handle)).list_patterns(limit=5, search="x")

    assert [p.id for p in page.patterns()] == ["p1"]


def test_error_status_is_returned_not_raised() -> None:
    api = client_with(httpx.MockTransport(lambda _: httpx.Response(400, json={"title": "bad"})))

    response = api.create({})

    assert response.status == 400
    assert response.json() == {"title": "bad"}


def test_unreachable_url_error_names_the_url() -> None:
    def refuse(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    try:
        client_with(httpx.MockTransport(refuse)).get("p1")
    except ConnectionError as exc:
        assert "http://api.test:8080/v1/api/patterns/p1" in str(exc)
    else:
        raise AssertionError("expected ConnectionError")
