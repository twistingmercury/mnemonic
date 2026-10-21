"""Pure checks that the project scaffold imports and its two core helpers work."""

import re

import pytest

from mnemonic_e2e.config import Settings
from mnemonic_e2e.wait import until

# The dev credential every service in docker-compose.yaml is configured with.
DEV_CREDENTIAL = "mnemonic_dev"


def test_settings_defaults() -> None:
    settings = Settings.from_env({})

    assert settings.api_url == "http://mnemonic-api:8080"
    assert settings.mcp_url == "http://mnemonic-mcp:8081"
    assert settings.mcp_ops_url == "http://mnemonic-mcp:8080"
    assert settings.enricher_url == "http://mnemonic-enricher:8080"
    assert settings.openai_stub_url == "http://openai_stub:8090"
    assert settings.metrics_port == 9090
    assert settings.database_url == "postgresql://mnemonic:mnemonic_dev@postgres:5432/mnemonic"
    assert settings.neo4j_uri == "bolt://neo4j:7687"
    assert settings.neo4j_user == "neo4j"
    assert settings.neo4j_password == DEV_CREDENTIAL
    assert settings.neo4j_database == "neo4j"
    assert settings.rabbitmq_url == "amqp://mnemonic:mnemonic_dev@rabbitmq:5672/"


def test_wait_until_times_out_with_message() -> None:
    with pytest.raises(TimeoutError, match=re.escape("pattern p1 to be enriched")) as info:
        until(
            lambda: "pending",
            "pattern p1 to be enriched",
            0.2,
            0.05,
            done=lambda s: s == "enriched",
        )

    assert "last observed: 'pending'" in str(info.value)
