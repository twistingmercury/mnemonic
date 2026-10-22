"""Fixtures shared by the stack tests. Each one is lazy: unit tests that ask for none of them need no stack."""

import os
from collections.abc import Callable, Iterator
from typing import Any

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.amqp import Amqp
from mnemonic_e2e.api import ApiClient, ApiResponse, Pattern, pattern_payload
from mnemonic_e2e.config import Settings
from mnemonic_e2e.db import Graph, Pg
from mnemonic_e2e.mcp import McpClient
from mnemonic_e2e.stub import StubClient

CreatePattern = Callable[[str, str], Pattern]
CreateWith = Callable[..., ApiResponse]
ListedIds = Callable[..., set[str]]


@pytest.fixture(scope="session")
def settings() -> Settings:
    return Settings.from_env(os.environ)


@pytest.fixture(scope="session")
def api(settings: Settings) -> ApiClient:
    return ApiClient(settings.api_url)


@pytest.fixture(scope="session")
def mcp(settings: Settings) -> McpClient:
    return McpClient(settings.mcp_url)


@pytest.fixture(scope="session")
def stub(settings: Settings) -> StubClient:
    return StubClient(settings.openai_stub_url)


@pytest.fixture(scope="session")
def pg(settings: Settings) -> Pg:
    return Pg(settings.database_url)


@pytest.fixture(scope="session")
def graph(settings: Settings) -> Iterator[Graph]:
    g = Graph(settings.neo4j_uri, settings.neo4j_user, settings.neo4j_password, settings.neo4j_database)
    yield g
    g.close()


@pytest.fixture(scope="session")
def amqp(settings: Settings) -> Amqp:
    return Amqp(settings.rabbitmq_url)


@pytest.fixture
def create_pattern(api: ApiClient) -> Iterator[CreatePattern]:
    """Create patterns through the API (202 expected) and delete them when the test ends."""
    created: list[str] = []

    def create(name: str, content: str) -> Pattern:
        response = api.create(pattern_payload(name, content))
        assert response.status == 202, f"create {name}: status {response.status}\nbody: {response.text}"
        pattern = response.pattern()
        created.append(pattern.id)
        return pattern

    yield create

    # Already gone (404) is fine: some tests delete their own pattern.
    for pattern_id in created:
        api.delete(pattern_id)


@pytest.fixture
def injected_failures(stub: StubClient) -> Iterator[StubClient]:
    """The stub, with every injected failure and delay cleared when the test ends so none leaks into another test."""
    yield stub
    stub.reset()


@pytest.fixture
def create_with(api: ApiClient) -> Iterator[CreateWith]:
    """Create patterns with chosen name, tags, description and content (202 expected); delete them at test end."""
    created: list[str] = []

    def create(content: str | None = None, name: str | None = None, **overrides: Any) -> ApiResponse:
        name = name or wait.unique_name("integ-api")
        content = content or "## Overview\n\nAPI test pattern.\n"
        response = api.create(pattern_payload(name, content, **overrides))
        assert response.status == 202, f"create {name}: status {response.status}\nbody: {response.text}"
        created.append(response.pattern().id)
        return response

    yield create

    # Already gone (404) is fine.
    for pattern_id in created:
        api.delete(pattern_id)


@pytest.fixture
def listed_ids(api: ApiClient) -> ListedIds:
    """List patterns with the given query parameters (200 expected) and return their ids."""

    def listed(**params: Any) -> set[str]:
        response = api.list_patterns(**params)
        assert response.status == 200, f"list {params}: status {response.status}\nbody: {response.text}"
        return {p.id for p in response.patterns()}

    return listed
