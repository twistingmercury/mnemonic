import pytest

from mnemonic_e2e.db import DIMENSIONS, Pg, unit_vector, vector_literal


def test_vector_literal_is_a_pgvector_text_value() -> None:
    assert vector_literal([1.0, 0.0, 0.5]) == "[1.0,0.0,0.5]"


def test_unit_vector_has_the_stub_dimensions_and_one_hot_value() -> None:
    vec = unit_vector(3)

    assert len(vec) == DIMENSIONS == 2000
    assert vec[3] == 1.0
    assert sum(vec) == 1.0


def test_unit_vector_rejects_an_out_of_range_dimension() -> None:
    with pytest.raises(ValueError, match="2000"):
        unit_vector(2000)


class FakeCursor:
    def __init__(self, row: tuple[object, ...] | None) -> None:
        self._row = row

    def fetchone(self) -> tuple[object, ...] | None:
        return self._row


class FakeConnection:
    """Stands in for a psycopg connection: records each statement, answers one row."""

    def __init__(self, row: tuple[object, ...] | None) -> None:
        self.row = row
        self.executed: list[tuple[str, tuple[object, ...]]] = []

    def __enter__(self) -> "FakeConnection":
        return self

    def __exit__(self, *exc: object) -> None:
        return None

    def execute(self, sql: str, params: tuple[object, ...]) -> FakeCursor:
        self.executed.append((sql, params))
        return FakeCursor(self.row)


@pytest.fixture
def fake_conn(monkeypatch: pytest.MonkeyPatch) -> FakeConnection:
    conn = FakeConnection(("3f2b6c1e-7a4d-4e8b-9c0d-1e2f3a4b5c6d",))
    monkeypatch.setattr("mnemonic_e2e.db.psycopg.connect", lambda dsn: conn)
    return conn


def test_seed_job_sets_max_attempts(fake_conn: FakeConnection) -> None:
    job_id = Pg("postgresql://x").seed_job("chunk-1", max_attempts=1)

    sql, params = fake_conn.executed[0]
    assert job_id == "3f2b6c1e-7a4d-4e8b-9c0d-1e2f3a4b5c6d"
    assert "max_attempts" in sql
    assert params == ("chunk-1", "pending", 0, 1)


def test_seed_job_defaults_to_the_api_max_attempts(fake_conn: FakeConnection) -> None:
    Pg("postgresql://x").seed_job("chunk-1")

    _, params = fake_conn.executed[0]
    assert params == ("chunk-1", "pending", 0, 3)
