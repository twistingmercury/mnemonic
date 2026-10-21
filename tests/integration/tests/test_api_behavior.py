"""API behavior that only shows against a real database and the running enricher.

These need the running compose stack. Every test makes its own patterns with
unique names, tags and tokens, so the tests share one stack in any order. Tests
that inject a stub failure run serially: the stub's reset is global.
"""

from collections.abc import Callable, Iterator
from typing import Any

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient, ApiResponse, Pattern, pattern_payload
from mnemonic_e2e.stub import StubClient
from mnemonic_e2e.tokens import TokenPool

ENRICH_TIMEOUT_S = 30.0
# The enricher's retry sweep runs every 15s: one full interval plus enrichment.
RETRY_WINDOW_S = 45.0
# The stub scores a one-word query against a whole chunk at about 0.17; unrelated text scores 0.
SEARCH_THRESHOLD = 0.1
# The section_title column is varchar(255).
OVERLONG_TITLE = "t" * 300

# Creates a pattern with payload overrides (202 expected) and returns the response; deleted when the test ends.
CreateWith = Callable[..., ApiResponse]


def body(token: str = "") -> str:
    """Valid, single-chunk content; the token, when given, is the only word the stub can match on."""
    return f"## Overview\n\nAPI behavior test pattern. {token}\n"


def two_section_content(first: str, second: str) -> str:
    """Content the API splits into two chunks titled first and second."""
    return f"[//]: pattern\n\n## {first}\n\nFirst body.\n\n[//]: pattern\n\n## {second}\n\nSecond body.\n"


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, tags and names included.
    return TokenPool(body("") + " " + two_section_content("one", "two") + " integ api behavior")


@pytest.fixture
def create_with(api: ApiClient) -> Iterator[CreateWith]:
    """Create patterns with chosen description, tags and content; delete them when the test ends."""
    created: list[str] = []

    def create(content: str | None = None, **overrides: Any) -> ApiResponse:
        name = wait.unique_name("integ-api")
        response = api.create(pattern_payload(name, content if content is not None else body(), **overrides))
        assert response.status == 202, f"create {name}: status {response.status}\nbody: {response.text}"
        created.append(response.pattern().id)
        return response

    yield create

    # Already gone (404) is fine.
    for pattern_id in created:
        api.delete(pattern_id)


def fetch(api: ApiClient, pattern_id: str) -> ApiResponse:
    response = api.get(pattern_id)
    assert response.status == 200, f"GET pattern {pattern_id}: status {response.status}\nbody: {response.text}"
    return response


def fetch_pattern(api: ApiClient, pattern_id: str) -> Pattern:
    return fetch(api, pattern_id).pattern()


def wait_status(api: ApiClient, pattern_id: str, wanted: str, timeout_s: float) -> Pattern:
    """Poll until the pattern's enrichment_status is wanted and return it."""
    return wait.until(
        lambda: fetch_pattern(api, pattern_id),
        f"pattern {pattern_id} enrichment_status to be {wanted}",
        timeout_s,
        done=lambda p: p.enrichment_status == wanted,
    )


def search_ids(api: ApiClient, query: str) -> list[str]:
    response = api.search(query, threshold=SEARCH_THRESHOLD)
    assert response.status == 200, f"API search {query!r}: status {response.status}\nbody: {response.text}"
    return response.pattern_ids()


def listed_ids(api: ApiClient, **params: Any) -> set[str]:
    response = api.list_patterns(**params)
    assert response.status == 200, f"list {params}: status {response.status}\nbody: {response.text}"
    return {p.id for p in response.patterns()}


def failing_pattern(
    api: ApiClient, stub: StubClient, tokens: TokenPool, create_with: CreateWith, timeout_s: float = ENRICH_TIMEOUT_S
) -> tuple[str, Pattern]:
    """Create a pattern whose first enrichment attempt is forced to fail; return its token and failed state."""
    token = tokens.new()
    # Compose runs the enricher with no in-call OpenAI retries, so the forced failure reaches the job.
    stub.fail(match=token, n=1)
    created = create_with(body(token)).pattern()
    failed = wait_status(api, created.id, "failed", timeout_s)
    assert stub.remaining(token) == 0, "the forced failure was never served"
    return token, failed


def test_search_excludes_pattern_until_enriched(
    api: ApiClient, injected_failures: StubClient, tokens: TokenPool, create_with: CreateWith
) -> None:
    token, failed = failing_pattern(api, injected_failures, tokens, create_with)

    assert failed.id not in search_ids(api, token), f"search returns pattern {failed.id} while it is failed"

    wait_status(api, failed.id, "enriched", RETRY_WINDOW_S)
    assert failed.id in search_ids(api, token), f"search misses pattern {failed.id} after it was enriched"


def test_failed_enrichment_reports_an_error(
    api: ApiClient, injected_failures: StubClient, tokens: TokenPool, create_with: CreateWith
) -> None:
    _, failed = failing_pattern(api, injected_failures, tokens, create_with)

    assert failed.enrichment_error, f"pattern {failed.id} is failed but enrichment_error is {failed.enrichment_error!r}"


def test_create_and_get_carry_the_submitted_fields(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    tags = [tokens.new(), tokens.new()]
    description = f"describes {tokens.new()}"
    content = body(tokens.new())

    created = create_with(content, description=description, tags=tags).json()
    got = fetch(api, created["id"]).json()

    for source, data in (("create", created), ("get", got)):
        assert data["content"] == content, f"{source}: content {data['content']!r}"
        assert data["description"] == description, f"{source}: description {data['description']!r}"
        assert data["tags"] == tags, f"{source}: tags {data['tags']!r}"

    # Checked on the create response only: by the time of a GET the enricher may have finished.
    assert created["enrichment_status"] == "pending", f"create: enrichment_status {created['enrichment_status']!r}"
    assert created["enriched_at"] is None, f"create: enriched_at {created['enriched_at']!r} while pending"


def test_list_filters(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    tag_a, tag_b, word = tokens.new(), tokens.new(), tokens.new()
    both = create_with(tags=[tag_a, tag_b]).pattern().id
    a_with_word = create_with(tags=[tag_a], description=f"mentions {word}").pattern().id
    word_only = create_with(tags=None, description=f"also mentions {word}").pattern().id
    create_with(tags=[tag_b])  # a decoy for every filter below

    assert listed_ids(api, tags=f"{tag_a},{tag_b}") == {both}, "tags=a,b must need both tags"
    assert listed_ids(api, tags=tag_a) == {both, a_with_word}, "tags=a must return every pattern tagged a"
    assert listed_ids(api, search=word) == {a_with_word, word_only}, "search must match the description alone"
    assert listed_ids(api, tags=tag_a, search=word) == {a_with_word}, "tags and search must both hold"


def test_list_pagination_walks_every_page_once(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    tag = tokens.new()
    created = {create_with(tags=[tag]).pattern().id for _ in range(3)}

    default = api.list_patterns(tags=tag).json()["pagination"]
    assert default["limit"] == 20, f"default pagination.limit {default['limit']}"
    assert default["has_more"] is False, "three patterns fit one default page"

    seen: list[str] = []
    cursor: str | None = None
    pages = 0
    while True:
        params: dict[str, Any] = {"tags": tag, "limit": 2}
        if cursor:
            params["cursor"] = cursor

        response = api.list_patterns(**params)
        assert response.status == 200, f"list page {pages}: status {response.status}\nbody: {response.text}"
        pagination = response.json()["pagination"]
        seen += [p.id for p in response.patterns()]
        pages += 1
        assert pagination["limit"] == 2, f"page {pages}: pagination.limit {pagination['limit']}"
        assert pages <= 2, f"more than two pages for three patterns: {seen}"

        if not pagination["has_more"]:
            assert pagination["next_cursor"] is None, f"last page carries next_cursor {pagination['next_cursor']!r}"
            break

        assert pagination["next_cursor"], f"page {pages} has_more but next_cursor is {pagination['next_cursor']!r}"
        cursor = pagination["next_cursor"]

    assert pages == 2, f"want 2 pages, walked {pages}"
    assert len(seen) == len(set(seen)), f"a pattern repeated across pages: {seen}"
    assert set(seen) == created, f"walk returned {seen}, want {created}"


def test_put_replaces_description_and_tags(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    created = create_with(description="to be removed", tags=[tokens.new()]).pattern()
    before = fetch(api, created.id).json()
    assert before["description"] and before["tags"], f"setup: description/tags not stored: {before}"

    updated = api.update(created.id, pattern_payload(created.name, body(), description=None, tags=None))
    assert updated.status == 204, f"update: status {updated.status}\nbody: {updated.text}"

    after = fetch(api, created.id).json()
    assert after["description"] is None, f"description after PUT without it: {after['description']!r}"
    assert after["tags"] == [], f"tags after PUT without them: {after['tags']!r}"


def test_failed_update_keeps_the_prior_state(api: ApiClient, create_with: CreateWith) -> None:
    content = two_section_content("Alpha", "Beta")
    created = create_with(content).pattern()
    # Settled first, so the enricher cannot touch the row between the two reads.
    wait_status(api, created.id, "enriched", ENRICH_TIMEOUT_S)
    before = fetch(api, created.id).json()
    titles = [c.section_title for c in api.chunks(created.id).chunk_list()]
    assert titles == ["Alpha", "Beta"], f"setup: chunk titles {titles}"

    broken = two_section_content(OVERLONG_TITLE, "Gamma")
    failed = api.update(created.id, pattern_payload(created.name, broken))
    assert failed.status == 500, (
        f"update with a {len(OVERLONG_TITLE)}-char title: status {failed.status}\n{failed.text}"
    )

    after = fetch(api, created.id).json()
    assert after["content"] == content, f"content changed by a failed update: {after['content']!r}"
    assert after["updated_at"] == before["updated_at"], "a failed update moved updated_at"
    assert after["enrichment_status"] == "enriched", f"status after failed update: {after['enrichment_status']!r}"
    assert [c.section_title for c in api.chunks(created.id).chunk_list()] == titles, "chunks changed by failed update"
