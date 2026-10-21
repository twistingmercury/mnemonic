"""Suite settings, read from MNEMONIC_-prefixed environment variables."""

from collections.abc import Mapping
from dataclasses import dataclass

# The defaults are the service names, ports and dev credentials of
# docker-compose.yaml, so the suite works inside that compose network as is.
_PREFIX = "MNEMONIC_"


@dataclass(frozen=True)
class Settings:
    api_url: str
    mcp_url: str
    mcp_ops_url: str
    enricher_url: str
    openai_stub_url: str
    metrics_port: int
    database_url: str
    neo4j_uri: str
    neo4j_user: str
    neo4j_password: str
    neo4j_database: str
    rabbitmq_url: str

    @classmethod
    def from_env(cls, environ: Mapping[str, str]) -> "Settings":
        """Build settings from environ; an unset or empty variable takes its default."""

        def text(name: str, default: str) -> str:
            return environ.get(_PREFIX + name) or default

        return cls(
            api_url=text("API_URL", "http://mnemonic-api:8080"),
            mcp_url=text("MCP_URL", "http://mnemonic-mcp:8081"),
            mcp_ops_url=text("MCP_OPS_URL", "http://mnemonic-mcp:8080"),
            enricher_url=text("ENRICHER_URL", "http://mnemonic-enricher:8080"),
            openai_stub_url=text("OPENAI_STUB_URL", "http://openai_stub:8090"),
            metrics_port=_port(text("METRICS_PORT", "9090")),
            database_url=text(
                "DATABASE_URL",
                "postgresql://mnemonic:mnemonic_dev@postgres:5432/mnemonic",
            ),
            neo4j_uri=text("NEO4J_URI", "bolt://neo4j:7687"),
            neo4j_user=text("NEO4J_USER", "neo4j"),
            neo4j_password=text("NEO4J_PASSWORD", "mnemonic_dev"),
            neo4j_database=text("NEO4J_DATABASE", "neo4j"),
            rabbitmq_url=text("RABBITMQ_URL", "amqp://mnemonic:mnemonic_dev@rabbitmq:5672/"),
        )


def _port(raw: str) -> int:
    try:
        return int(raw)
    except ValueError as exc:
        raise ValueError(f"{_PREFIX}METRICS_PORT must be a number, got {raw!r}") from exc
