# Runner image for the Python e2e suite. It only needs the compose network:
# the suite talks to the stack by service name.
FROM ghcr.io/astral-sh/uv:python3.14-bookworm-slim

RUN useradd --create-home --uid 10001 runner
WORKDIR /suite
RUN chown runner:runner /suite
USER runner

# Compile nothing and never reach for the network at run time.
ENV UV_COMPILE_BYTECODE=0 UV_NO_CACHE=1 UV_PYTHON_DOWNLOADS=never

COPY --chown=runner:runner pyproject.toml uv.lock ./
RUN uv sync --frozen --no-install-project

COPY --chown=runner:runner src ./src
COPY --chown=runner:runner tests ./tests
RUN uv sync --frozen

ENTRYPOINT ["uv", "run", "--no-sync", "pytest"]
