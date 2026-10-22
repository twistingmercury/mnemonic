"""API list ordering and filtering, the content limit, and the 404 and 409 answers.

These need the running compose stack. Every test makes its own patterns with
unique names, tags and tokens, so the tests share one stack in any order. The
list endpoint has no enrichment-status parameter, so no test filters on one.
"""

import uuid
from collections.abc import Callable

import pytest

from mnemonic_e2e.api import ApiClient, ApiResponse, pattern_payload
from mnemonic_e2e.tokens import TokenPool

# The create_with fixture (conftest.py): payload overrides -> the create response.
CreateWith = Callable[..., ApiResponse]
# The listed_ids fixture (conftest.py): query parameters -> the listed pattern ids.
ListedIds = Callable[..., set[str]]

CONTENT_LIMIT = 100_000


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, tags and names included.
    return TokenPool("## Overview API test pattern integ api")


def content_of_size(size: int) -> str:
    """Valid one-section content of exactly size bytes."""
    heading = "## Overview\n\n"
    return heading + "a" * (size - len(heading))


def test_list_is_ordered_by_name(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    tag = tokens.new()
    # Created out of order so insertion order cannot pass the test.
    for letter in ("c", "a", "b"):
        create_with(name=f"{tag}-{letter}", tags=[tag])

    names = [p.name for p in api.list_patterns(tags=tag).patterns()]

    assert names == [f"{tag}-a", f"{tag}-b", f"{tag}-c"], f"list order {names}"


def test_search_matches_a_word_only_in_the_name(
    create_with: CreateWith, listed_ids: ListedIds, tokens: TokenPool
) -> None:
    word = tokens.new()
    named = create_with(name=f"integ-api-{word}").pattern().id
    create_with()  # a decoy that lacks the word

    assert listed_ids(search=word) == {named}, "search must match a word found only in the name"


def test_unmatched_tag_lists_nothing(create_with: CreateWith, listed_ids: ListedIds, tokens: TokenPool) -> None:
    create_with(tags=[tokens.new()])  # a decoy carrying some other tag

    assert listed_ids(tags=tokens.new()) == set(), "a tag no pattern carries must list nothing"


def test_content_limit_is_100000_characters(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    create_with(content_of_size(CONTENT_LIMIT))

    over = api.create(pattern_payload(f"integ-api-{tokens.new()}", content_of_size(CONTENT_LIMIT + 1)))

    assert over.status == 400, f"content of {CONTENT_LIMIT + 1} characters: status {over.status}\nbody: {over.text}"


def test_duplicate_name_is_rejected(api: ApiClient, create_with: CreateWith, tokens: TokenPool) -> None:
    name = f"integ-api-{tokens.new()}"
    create_with(name=name)

    duplicate = api.create(pattern_payload(name, "## Overview\n\nAPI test pattern.\n"))

    assert duplicate.status == 409, f"duplicate name {name}: status {duplicate.status}\nbody: {duplicate.text}"


def test_update_of_unknown_id_is_404(api: ApiClient, tokens: TokenPool) -> None:
    unknown = str(uuid.uuid4())

    response = api.update(unknown, pattern_payload(f"integ-api-{tokens.new()}", "## Overview\n\nAPI test pattern.\n"))

    assert response.status == 404, f"update of unknown id {unknown}: status {response.status}\nbody: {response.text}"


def test_delete_of_unknown_id_is_404(api: ApiClient) -> None:
    unknown = str(uuid.uuid4())

    response = api.delete(unknown)

    assert response.status == 404, f"delete of unknown id {unknown}: status {response.status}\nbody: {response.text}"
