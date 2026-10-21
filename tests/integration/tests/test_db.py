import pytest

from mnemonic_e2e.db import DIMENSIONS, unit_vector, vector_literal


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
