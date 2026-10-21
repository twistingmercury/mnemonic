import pytest

from mnemonic_e2e.db import DIMENSIONS
from mnemonic_e2e.tokens import TokenPool, stub_dimension, words


@pytest.mark.parametrize(
    ("word", "fnv1a_32"),
    [("", 0x811C9DC5), ("a", 0xE40C292C), ("foobar", 0xBF9CF968)],
)
def test_stub_dimension_is_fnv1a_32_modulo_the_stub_dimensions(word: str, fnv1a_32: int) -> None:
    assert stub_dimension(word) == fnv1a_32 % DIMENSIONS


def test_words_lowercases_and_splits_on_anything_but_letters_and_digits() -> None:
    assert words("Integ-a-TOK12 | integration-test, fmt.Errorf %w") == [
        "integ",
        "a",
        "tok12",
        "integration",
        "test",
        "fmt",
        "errorf",
        "w",
    ]


def test_tokens_look_like_tok_plus_twelve_hex_digits() -> None:
    token = TokenPool("").new()

    assert token.startswith("tok")
    assert len(token) == 15
    int(token[3:], 16)


def test_a_token_never_shares_a_dimension_with_the_shared_text_or_another_token() -> None:
    shared = "integ a b c integration test errors wrapping core approach"
    pool = TokenPool(shared)
    taken = {stub_dimension(word) for word in words(shared)}

    for _ in range(300):
        dimension = stub_dimension(pool.new())
        assert dimension not in taken
        taken.add(dimension)
