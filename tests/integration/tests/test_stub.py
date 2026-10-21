import httpx
import pytest

from mnemonic_e2e.stub import StubClient


def test_fail_posts_match_and_count() -> None:
    seen: list[httpx.Request] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(204)

    StubClient("http://stub.test:8090", transport=httpx.MockTransport(handle)).fail("tokabc", 2)

    assert seen[0].method == "POST"
    assert seen[0].url.path == "/control/fail"
    assert dict(seen[0].url.params) == {"match": "tokabc", "n": "2"}


def test_reset_posts_to_control_reset() -> None:
    seen: list[httpx.Request] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(204)

    StubClient("http://stub.test:8090", transport=httpx.MockTransport(handle)).reset()

    assert (seen[0].method, seen[0].url.path) == ("POST", "/control/reset")


def test_rejected_control_call_raises_with_the_body() -> None:
    stub = StubClient(
        "http://stub.test:8090",
        transport=httpx.MockTransport(lambda _: httpx.Response(400, text="bad n")),
    )

    with pytest.raises(RuntimeError, match="bad n"):
        stub.fail("x", -1)


def test_healthy_reflects_the_healthz_status() -> None:
    ok = StubClient(
        "http://stub.test:8090",
        transport=httpx.MockTransport(lambda _: httpx.Response(200)),
    )
    down = StubClient(
        "http://stub.test:8090",
        transport=httpx.MockTransport(lambda _: httpx.Response(503)),
    )

    assert ok.healthy() is True
    assert down.healthy() is False
