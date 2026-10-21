"""Direct database access: Postgres reads and seeding, Neo4j lookups.

Reads verify what the services stored; seeding builds states the services
cannot be driven into through their public surface.
"""

import uuid
from dataclasses import dataclass

import psycopg
from neo4j import GraphDatabase

# The embedding size the services request from the OpenAI stub (migration 000010).
DIMENSIONS = 2000


def vector_literal(values: list[float]) -> str:
    """Render values as a pgvector text literal."""
    return "[" + ",".join(str(v) for v in values) + "]"


def unit_vector(dimension: int) -> list[float]:
    """Return a vector of DIMENSIONS zeros with a 1.0 at dimension."""
    if not 0 <= dimension < DIMENSIONS:
        raise ValueError(f"dimension must be in [0, {DIMENSIONS}), got {dimension}")

    vec = [0.0] * DIMENSIONS
    vec[dimension] = 1.0
    return vec


@dataclass(frozen=True)
class Job:
    id: str
    chunk_id: str | None
    pattern_id: str | None
    status: str
    attempts: int
    last_error: str | None


@dataclass(frozen=True)
class Chunk:
    id: str
    chunk_index: int
    section_title: str
    enrichment_status: str
    has_embedding: bool


_JOBS_FOR_PATTERN = """
    select j.id::text, j.chunk_id::text, j.pattern_id::text, j.status, j.attempts, j.last_error
    from enrichment_jobs j
    left join pattern_chunks c on c.id = j.chunk_id
    where j.pattern_id = %s or c.pattern_id = %s
    order by j.created_at
"""
_CHUNKS_FOR_PATTERN = """
    select id::text, chunk_index, section_title, enrichment_status, embedding is not null
    from pattern_chunks where pattern_id = %s order by chunk_index
"""
_PATTERN_STATUS = "select enrichment_status from patterns where id = %s"
_INSERT_PATTERN = """
    insert into patterns (name, content, entity_type, language, domain, enrichment_status)
    values (%s, %s, 'go-pattern', 'go', 'backend', %s) returning id::text
"""
_INSERT_CHUNK = """
    insert into pattern_chunks (pattern_id, section_title, chunk_index, content, embedding, enrichment_status)
    values (%s, %s, %s, %s, %s::vector, %s) returning id::text
"""
_INSERT_JOB = "insert into enrichment_jobs (chunk_id, status, attempts) values (%s, %s, %s) returning id::text"
_DELETE_PATTERN = "delete from patterns where id = %s"


class Pg:
    def __init__(self, dsn: str) -> None:
        self._dsn = dsn

    def jobs_for_pattern(self, pattern_id: str) -> list[Job]:
        """Every enrichment job of the pattern or of its chunks, oldest first."""
        with psycopg.connect(self._dsn) as conn:
            rows = conn.execute(_JOBS_FOR_PATTERN, (pattern_id, pattern_id)).fetchall()
        return [Job(*row) for row in rows]

    def chunks_for_pattern(self, pattern_id: str) -> list[Chunk]:
        with psycopg.connect(self._dsn) as conn:
            rows = conn.execute(_CHUNKS_FOR_PATTERN, (pattern_id,)).fetchall()
        return [Chunk(*row) for row in rows]

    def pattern_status(self, pattern_id: str) -> str | None:
        """The pattern's enrichment_status, or None when the row does not exist."""
        with psycopg.connect(self._dsn) as conn:
            row = conn.execute(_PATTERN_STATUS, (pattern_id,)).fetchone()
        return None if row is None else str(row[0])

    def seed_pattern(self, name: str, content: str = "seeded", status: str = "pending") -> str:
        """Insert a pattern row directly and return its id."""
        return self._insert(_INSERT_PATTERN, (name, content, status))

    def seed_chunk(
        self,
        pattern_id: str,
        index: int = 0,
        title: str = "Seeded",
        content: str = "seeded",
        embedding: list[float] | None = None,
        status: str = "pending",
    ) -> str:
        """Insert a chunk of pattern_id, with an embedding when one is given, and return its id."""
        vector = None if embedding is None else vector_literal(embedding)
        return self._insert(_INSERT_CHUNK, (pattern_id, title, index, content, vector, status))

    def seed_job(self, chunk_id: str, status: str = "pending", attempts: int = 0) -> str:
        """Insert an enrichment job for chunk_id and return its id."""
        return self._insert(_INSERT_JOB, (chunk_id, status, attempts))

    def delete_pattern(self, pattern_id: str) -> None:
        """Remove a seeded pattern; its chunks and jobs go with it (on delete cascade)."""
        with psycopg.connect(self._dsn) as conn:
            conn.execute(_DELETE_PATTERN, (pattern_id,))

    def _insert(self, sql: str, params: tuple[object, ...]) -> str:
        with psycopg.connect(self._dsn) as conn:
            row = conn.execute(sql, params).fetchone()
        if row is None:
            raise RuntimeError("insert returned no id")
        return str(uuid.UUID(row[0]))


_PATTERN_COUNT = "MATCH (p:Pattern {id: $id}) RETURN count(p) AS n"
_CONCEPTS_OF_PATTERN = "MATCH (c:Concept)-[:MENTIONED_IN]->(:Pattern {id: $id}) RETURN c.name AS name ORDER BY name"
_RELATED_OF_PATTERN = "MATCH (:Pattern {id: $id})-[:RELATED_TO]-(o:Pattern) RETURN o.id AS id ORDER BY id"
_CONCEPTS_WITH_PREFIX = "MATCH (c:Concept) WHERE c.name STARTS WITH $prefix RETURN c.name AS name ORDER BY name"


class Graph:
    def __init__(self, uri: str, user: str, password: str, database: str) -> None:
        self._driver = GraphDatabase.driver(uri, auth=(user, password))
        self._database = database

    def close(self) -> None:
        self._driver.close()

    def pattern_count(self, pattern_id: str) -> int:
        records, _, _ = self._driver.execute_query(_PATTERN_COUNT, id=pattern_id, database_=self._database)
        return int(records[0]["n"])

    def concepts_of(self, pattern_id: str) -> list[str]:
        """Names of the concepts mentioned in the pattern, sorted."""
        return self._names(_CONCEPTS_OF_PATTERN, "name", id=pattern_id)

    def related_pattern_ids(self, pattern_id: str) -> list[str]:
        return self._names(_RELATED_OF_PATTERN, "id", id=pattern_id)

    def concepts_with_prefix(self, prefix: str) -> list[str]:
        return self._names(_CONCEPTS_WITH_PREFIX, "name", prefix=prefix)

    def _names(self, query: str, key: str, **params: str) -> list[str]:
        records, _, _ = self._driver.execute_query(query, parameters_=params, database_=self._database)
        return [str(record[key]) for record in records]
