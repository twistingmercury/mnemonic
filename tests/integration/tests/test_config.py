import pytest

from mnemonic_e2e.config import Settings


def test_prefixed_variables_override_defaults() -> None:
    settings = Settings.from_env(
        {
            "MNEMONIC_API_URL": "http://localhost:1",
            "MNEMONIC_METRICS_PORT": "1234",
            "MNEMONIC_NEO4J_DATABASE": "other",
        }
    )

    assert settings.api_url == "http://localhost:1"
    assert settings.metrics_port == 1234
    assert settings.neo4j_database == "other"


def test_unprefixed_variables_are_ignored() -> None:
    settings = Settings.from_env({"API_URL": "http://wrong", "DATABASE_URL": "postgresql://wrong"})

    assert settings.api_url == "http://mnemonic-api:8080"
    assert settings.database_url.startswith("postgresql://mnemonic:")


def test_empty_value_falls_back_to_default() -> None:
    assert Settings.from_env({"MNEMONIC_MCP_URL": ""}).mcp_url == "http://mnemonic-mcp:8081"


def test_non_numeric_metrics_port_names_the_variable() -> None:
    with pytest.raises(ValueError, match="MNEMONIC_METRICS_PORT"):
        Settings.from_env({"MNEMONIC_METRICS_PORT": "nine"})
