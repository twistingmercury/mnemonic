"""MCP search ranking and filters, and the create and update lifecycle of a pattern's chunks and jobs.

These need the running compose stack. The OpenAI stub embeds a text as a unit
vector with one count per distinct word, so the cosine similarity between a
query and a chunk is (words they share) / sqrt(query words * chunk words). A
chunk is embedded as "name | tags | title" plus its body: the name contributes
two words (integ and a unique token), the title one (overview). The query words
are tokens only the test's own patterns contain, so every other pattern in the
shared database scores 0 and a threshold above 0 keeps it out of the results.
"""

import re
from collections.abc import Callable
from dataclasses import dataclass

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient, ApiResponse, Pattern, pattern_payload
from mnemonic_e2e.db import Pg
from mnemonic_e2e.mcp import McpClient
from mnemonic_e2e.stub import StubClient
from mnemonic_e2e.tokens import TokenPool

# Between the middling match (0.35) and the best one (0.63) built by the ranked fixture.
THRESHOLD = 0.3
# How long the stub holds the embedding of an updated pattern's last chunk, so the pattern stays pending.
# The enricher gives an embedding request 30 s (service/openai/embedding.go), so the hold is far under it.
HOLD_MS = 5000
# The section_title column is varchar(255).
OVERLONG_TITLE = "t" * 300

# The create_with fixture (conftest.py): payload overrides -> the create response.
CreateWith = Callable[..., ApiResponse]


def body(words: str) -> str:
    """Single-chunk content whose body is exactly these words."""
    return f"## Overview\n\n{words}\n"


def sections(titles: list[str]) -> str:
    """Content the API splits into one chunk per title."""
    return "".join(f"[//]: pattern\n\n## {title}\n\nBody of {title}.\n\n" for title in titles)


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, tags and names included.
    return TokenPool(body("") + " integ integration test go python concepts")


def name_with(token: str) -> str:
    return f"integ-{token}"


def found(text: str, names: list[str]) -> list[str]:
    """The names a search result lists as direct matches, in the order it lists them."""
    starts = {n: m.start() for n in names if (m := re.search(rf"## {re.escape(n)} \(\d+% match\)", text))}
    return sorted(starts, key=starts.__getitem__)


@dataclass(frozen=True)
class Ranked:
    query: str
    best: str
    middling: str
    unrelated: str


@pytest.fixture
def ranked(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> Ranked:
    """Three enriched patterns whose similarity to the query is about 0.63, 0.35 and 0.

    With no tags a chunk has the words integ, its name token and overview, plus its body.
    best has the two query words (2 shared / sqrt(2 * 5)); middling has one (1 / sqrt(2 * 4)).
    """
    first, second = tokens.new(), tokens.new()
    # Created weakest first, so insertion order is not the expected order.
    names = {
        "unrelated": name_with(tokens.new()),
        "middling": name_with(tokens.new()),
        "best": name_with(tokens.new()),
    }
    bodies = {"unrelated": tokens.new(), "middling": first, "best": f"{first} {second}"}
    for key, name in names.items():
        created = create_with(body(bodies[key]), name=name, tags=None).pattern()
        wait.enriched(api, created.id)

    return Ranked(f"{first} {second}", names["best"], names["middling"], names["unrelated"])


def test_search_orders_by_similarity_and_applies_the_threshold(mcp: McpClient, ranked: Ranked) -> None:
    names = [ranked.best, ranked.middling, ranked.unrelated]

    text = mcp.call_ok("search_patterns", {"query": ranked.query, "threshold": THRESHOLD})
    assert found(text, names) == [ranked.best, ranked.middling], f"want best then middling only:\n{text}"

    strict = mcp.call_ok("search_patterns", {"query": ranked.query, "threshold": 0.5})
    assert found(strict, names) == [ranked.best], f"a threshold of 0.5 must drop the middling match:\n{strict}"


def test_search_respects_the_limit(mcp: McpClient, ranked: Ranked) -> None:
    args = {"query": ranked.query, "threshold": THRESHOLD}
    assert ranked.middling in mcp.call_ok("search_patterns", args), "setup: the middling match is not found"

    text = mcp.call_ok("search_patterns", {**args, "limit": 1})
    assert found(text, [ranked.best, ranked.middling]) == [ranked.best], f"limit 1 must keep only the best:\n{text}"


def test_search_filters_by_language_and_tags(
    api: ApiClient, mcp: McpClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    query, tag_a, tag_b = tokens.new(), tokens.new(), tokens.new()
    go_both = name_with(tokens.new())
    python_both = name_with(tokens.new())
    go_one = name_with(tokens.new())
    # All three contain the query word, so only a filter can tell them apart.
    for name, language, tags in (
        (go_both, "go", [tag_a, tag_b]),
        (python_both, "python", [tag_a, tag_b]),
        (go_one, "go", [tag_a]),
    ):
        created = create_with(body(query), name=name, language=language, tags=tags).pattern()
        wait.enriched(api, created.id)
    names = [go_both, python_both, go_one]

    def search(**filters: object) -> set[str]:
        text = mcp.call_ok("search_patterns", {"query": query, "threshold": THRESHOLD, **filters})
        return set(found(text, names))

    assert search() == set(names), "setup: without a filter all three match"
    assert search(language="python") == {python_both}
    assert search(tags=[tag_a]) == set(names)
    assert search(tags=[tag_a, tag_b]) == {go_both, python_both}, "tags are an AND filter"
    assert search(tags=[tag_a, tag_b], language="go") == {go_both}
    assert search(tags=[tokens.new()]) == set(), "a tag no pattern carries matches nothing"


def test_search_result_shows_section_tags_body_and_match(
    api: ApiClient, mcp: McpClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    query, tag = tokens.new(), tokens.new()
    name = name_with(tokens.new())
    wait.enriched(api, create_with(body(query), name=name, tags=[tag]).pattern().id)

    text = mcp.call_ok("search_patterns", {"query": query, "threshold": THRESHOLD})

    # The chunk has five words (integ, the name token, the tag, overview, the query word) and shares one: 1/sqrt(5).
    assert f"## {name} (45% match)\n\n**Section:** Overview\n\n**Tags:** {tag}\n\n" in text
    assert f"{tag}\n\n## Overview\n\n{query}\n" in text, f"the chunk body is not shown under its tags:\n{text}"


def test_search_expands_through_the_graph_and_filters_the_expansion(
    api: ApiClient, mcp: McpClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    query, concept, filler, tag, other_tag = (tokens.new() for _ in range(5))
    seed, neighbour = name_with(tokens.new()), name_with(tokens.new())
    # The seed matches the query; the neighbour shares only the concept, so only the graph can lead to it.
    seed_content = f"## Overview\n\n{query}\n\nConcepts: {concept}\n"
    neighbour_content = f"## Overview\n\n{filler}\n\nConcepts: {concept}\n"
    created = [
        create_with(seed_content, name=seed, language="go", tags=[tag, other_tag]).pattern(),
        create_with(neighbour_content, name=neighbour, language="python", tags=[tag]).pattern(),
    ]
    for pattern in created:
        wait.enriched(api, pattern.id)

    def search(**filters: object) -> str:
        return mcp.call_ok("search_patterns", {"query": query, "threshold": THRESHOLD, **filters})

    text = search()
    assert found(text, [seed, neighbour]) == [seed], f"the neighbour must not be a direct match:\n{text}"
    graph_section = text.partition("### Related Patterns (via graph)")[2]
    want = f"## {neighbour} (similarity: 1.00)\n\n**Found via:** {seed}\n**Shared concepts:** {concept}\n"
    assert want in graph_section, f"want the neighbour found via the seed:\n{text}"

    assert neighbour not in search(language="go"), "the neighbour is python, so a go search must not expand to it"
    assert neighbour not in search(tags=[other_tag]), "the neighbour lacks the tag, so the expansion must drop it"
    assert neighbour in search(tags=[tag]), "setup: a tag both carry keeps the expansion"


def update_held(api: ApiClient, stub: StubClient, pattern: Pattern, token: str) -> list[str]:
    """Update the pattern to three sections, the last one's embedding held by the stub; return the titles."""
    stub.delay(token, HOLD_MS)
    titles = ["Gamma", "Delta", f"Held {token}"]
    response = api.update(pattern.id, pattern_payload(pattern.name, sections(titles)))
    assert response.status == 204, f"update: status {response.status}\nbody: {response.text}"
    return titles


def assert_reset(api: ApiClient, pg: Pg, pattern: Pattern, titles: list[str], old: tuple[set[str], set[str]]) -> None:
    """The pattern is pending with its error and enriched_at cleared, and its chunks and jobs are all new."""
    old_chunks, old_jobs = old
    reset = api.get(pattern.id).pattern()
    assert reset.enrichment_status == "pending", f"status after the update: {reset.enrichment_status!r}"
    assert reset.enrichment_error is None, f"enrichment_error survived the update: {reset.enrichment_error!r}"
    assert reset.enriched_at is None, f"enriched_at survived the update: {reset.enriched_at!r}"

    chunks = pg.chunks_for_pattern(pattern.id)
    assert [c.section_title for c in chunks] == titles
    assert not {c.id for c in chunks} & old_chunks, "an old chunk survived the update"

    jobs = pg.jobs_for_pattern(pattern.id)
    assert {j.chunk_id for j in jobs} == {c.id for c in chunks}, "the new chunks do not have exactly one job each"
    assert len(jobs) == len(titles)
    assert not {j.id for j in jobs} & old_jobs, "an old job survived the update"


def test_update_resets_an_enriched_pattern_to_pending(
    api: ApiClient, pg: Pg, injected_failures: StubClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    created = create_with(sections(["Alpha", "Beta"])).pattern()
    wait.enriched(api, created.id)
    old = ({c.id for c in pg.chunks_for_pattern(created.id)}, {j.id for j in pg.jobs_for_pattern(created.id)})
    assert len(old[0]) == len(old[1]) == 2, "setup: the enriched pattern has two chunks and two jobs"

    titles = update_held(api, injected_failures, created, tokens.new())

    assert_reset(api, pg, created, titles, old)


def test_update_resets_a_failed_pattern(
    api: ApiClient, pg: Pg, injected_failures: StubClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    token = tokens.new()
    # Compose runs the enricher with no in-call OpenAI retries, so the forced failure reaches the job.
    injected_failures.fail(match=token, n=1)
    created = create_with(body(token)).pattern()
    failed = wait.until(
        lambda: api.get(created.id).pattern(),
        f"pattern {created.id} to fail",
        done=lambda p: p.enrichment_status == "failed",
    )
    assert failed.enrichment_error, "setup: the failed pattern has no enrichment_error"
    old = ({c.id for c in pg.chunks_for_pattern(created.id)}, {j.id for j in pg.jobs_for_pattern(created.id)})

    titles = update_held(api, injected_failures, created, tokens.new())

    assert_reset(api, pg, created, titles, old)


def test_failed_create_frees_the_name(api: ApiClient, pg: Pg, create_with: CreateWith) -> None:
    name = wait.unique_name("integ-lifecycle")

    # Issue #22: the stack answers 500, not 400, for a section title the column cannot hold.
    failed = api.create(pattern_payload(name, sections([OVERLONG_TITLE, "Gamma"])))
    assert failed.status == 500, (
        f"create with a {len(OVERLONG_TITLE)}-char title: status {failed.status}\n{failed.text}"
    )

    # create_with asserts the 202: the name is free again.
    created = create_with(sections(["Alpha", "Beta"]), name=name).pattern()

    assert [c.section_title for c in pg.chunks_for_pattern(created.id)] == ["Alpha", "Beta"]
    assert len(pg.jobs_for_pattern(created.id)) == 2


def test_created_pattern_has_one_job_per_chunk_with_no_attempts(pg: Pg, create_with: CreateWith) -> None:
    created = create_with(sections(["Alpha", "Beta", "Gamma"])).pattern()

    chunks = pg.chunks_for_pattern(created.id)
    jobs = pg.jobs_for_pattern(created.id)

    assert len(chunks) == len(jobs) == 3
    assert {j.chunk_id for j in jobs} == {c.id for c in chunks}, "not exactly one job per chunk"
    assert {j.attempts for j in jobs} == {0}, "no enrichment has failed, so no job has an attempt"
