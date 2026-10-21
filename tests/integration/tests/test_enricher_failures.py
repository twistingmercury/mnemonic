"""Enricher failure paths, driven straight through RabbitMQ and checked in Postgres and Neo4j.

These need the running compose stack. Each test seeds its own pattern, chunk and
job with a unique token, publishes job messages itself, and clears the stub's
injected failures afterwards, so the tests share one stack in any order.

The retry sweep (every 15s) claims a pending job only when it is waiting for a
retry (attempts > 0) or has gone two minutes without a delivery starting it. A
seeded job starts with attempts 0, so within these tests only the message the
test publishes can start it; after a terminal failure it is `failed`, which the
sweep never claims. The assertions are on final states only, so even a sweep
that took a job first would leave them true.
"""

import json
import uuid
from collections.abc import Callable, Iterator
from dataclasses import dataclass

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.amqp import ENRICHMENT_QUEUE, Amqp
from mnemonic_e2e.api import ApiClient
from mnemonic_e2e.db import Graph, Job, Pg
from mnemonic_e2e.stub import StubClient
from mnemonic_e2e.tokens import TokenPool

JOB_TIMEOUT_S = 30.0
FINAL_JOB_STATES = ("completed", "failed")


@dataclass(frozen=True)
class SeededJob:
    pattern_id: str
    chunk_id: str
    job_id: str
    token: str


SeedJob = Callable[..., SeededJob]


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    return TokenPool(seeded_content("") + " integ seed")


def seeded_content(token: str) -> str:
    """Chunk content holding the token (so a stub failure can target it) and a concept line for extraction."""
    return f"Seeded enrichment job {token}.\n\nConcepts: {token}-seeded\n"


@pytest.fixture
def seed_job(pg: Pg, api: ApiClient, tokens: TokenPool) -> Iterator[SeedJob]:
    """Seed a pattern, one chunk and its job in Postgres; delete them, and any graph node, when the test ends."""
    seeded: list[str] = []

    def seed(max_attempts: int = 3) -> SeededJob:
        token = tokens.new()
        pattern_id = pg.seed_pattern(f"integ-seed-{token}", content=seeded_content(token))
        seeded.append(pattern_id)
        chunk_id = pg.seed_chunk(pattern_id, content=seeded_content(token))
        job_id = pg.seed_job(chunk_id, max_attempts=max_attempts)
        return SeededJob(pattern_id, chunk_id, job_id, token)

    yield seed

    # The API delete removes the row (chunks and jobs cascade) and the graph node, if enrichment made one.
    for pattern_id in seeded:
        api.delete(pattern_id)


def publish_job(amqp: Amqp, job_id: str) -> None:
    """Publish a job message exactly as the API does: {"job_id": "<uuid>"} on the default exchange."""
    amqp.publish_raw(ENRICHMENT_QUEUE, json.dumps({"job_id": job_id}).encode())


def wait_final(pg: Pg, seeded: SeededJob) -> Job:
    """Poll until the seeded job is completed or failed and return it."""

    def job() -> Job | None:
        jobs = pg.jobs_for_pattern(seeded.pattern_id)
        return next((j for j in jobs if j.id == seeded.job_id), None)

    final = wait.until(
        job,
        f"job {seeded.job_id} to finish",
        JOB_TIMEOUT_S,
        done=lambda j: j is not None and j.status in FINAL_JOB_STATES,
    )
    assert final is not None
    return final


def assert_enriched(pg: Pg, graph: Graph, seeded: SeededJob) -> None:
    """The seeded job completed: chunk embedded, pattern enriched, pattern and concept in the graph."""
    job = wait_final(pg, seeded)
    assert job.status == "completed", f"job {seeded.job_id} ended {job.status}: {job.last_error}"

    chunks = pg.chunks_for_pattern(seeded.pattern_id)
    assert [(c.enrichment_status, c.has_embedding) for c in chunks] == [("enriched", True)]
    assert pg.pattern_status(seeded.pattern_id) == "enriched"
    assert graph.pattern_count(seeded.pattern_id) == 1
    assert graph.concepts_of(seeded.pattern_id) == [f"{seeded.token}-seeded"]


def test_terminal_failure_marks_job_failed(
    pg: Pg, graph: Graph, amqp: Amqp, injected_failures: StubClient, seed_job: SeedJob
) -> None:
    seeded = seed_job(max_attempts=1)
    # Compose runs the enricher with no in-call OpenAI retries, so one forced failure fails the job's only attempt.
    injected_failures.fail(match=seeded.token, n=1)

    publish_job(amqp, seeded.job_id)

    job = wait_final(pg, seeded)
    assert job.status == "failed", f"job {seeded.job_id} ended {job.status}, want failed"
    assert job.attempts == 1
    assert job.last_error, "a failed job has no last_error"
    assert injected_failures.remaining(seeded.token) == 0, "the forced failure was never served"
    assert pg.pattern_status(seeded.pattern_id) == "failed"
    assert [c.enrichment_status for c in pg.chunks_for_pattern(seeded.pattern_id)] == ["failed"]
    assert graph.pattern_count(seeded.pattern_id) == 0, "a failed embedding still wrote a graph node"


def test_malformed_message_does_not_wedge_consumer(pg: Pg, graph: Graph, amqp: Amqp, seed_job: SeedJob) -> None:
    seeded = seed_job()

    amqp.publish_raw(ENRICHMENT_QUEUE, b"not-json")
    publish_job(amqp, seeded.job_id)

    assert_enriched(pg, graph, seeded)


def test_unknown_job_id_is_acked_and_next_job_completes(pg: Pg, graph: Graph, amqp: Amqp, seed_job: SeedJob) -> None:
    seeded = seed_job()

    publish_job(amqp, str(uuid.uuid4()))
    publish_job(amqp, seeded.job_id)

    assert_enriched(pg, graph, seeded)
