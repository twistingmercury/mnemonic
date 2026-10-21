.PHONY: local build tests-unit tests-bench analyze help tests-db-graph

GIT_COMMIT := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%d)
LD_FLAGS="-s -w \
	-X 'github.com/twistingmercury/librarian-enricher/internal/version.version=${GIT_TAG}' \
	-X 'github.com/twistingmercury/librarian-enricher/internal/version.buildDate=${BUILD_DATE}' \
	-X 'github.com/twistingmercury/librarian-enricher/internal/version.commit=${GIT_COMMIT}'"

default: help

local: ## Build binary locally (no tests, no Docker)
	mkdir -p .bin
	cd src && go build --ldflags ${LD_FLAGS} -o ../.bin/mnemonic-enricher ./cmd/main

build: ## Build Docker image locally using the full CI build script
	LOCAL=1 src/build/build.sh

analyze: ## Run linters, formatters, security scanners, etc
	cd src && goimports -w . && golangci-lint run && govulncheck ./cmd/... ./internal/... && gosec -quiet -exclude-dir=tests ./...

tests-unit: ## Run the unit tests
	cd src && go test ./... -coverprofile=../coverage.out && go tool cover -html=../coverage.out

tests-bench: ## Run benchmark tests
	cd src && go test ./... -bench=. -benchmem -run=^$$

tests-db-graph: ## Run graph repository integration tests against NEO4J_URI (default bolt://localhost:17688)
	cd src && go test -tags integration ./internal/repository/graph/...

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
