OPA_VERSION   ?= 1.4.2
ENVOY_IMAGE   ?= envoyproxy/envoy:v1.34.1
COMPOSE_FILE  := deploy/docker-compose.yaml
STUB_COMPOSE  := docker compose -f gateway/test/compose.stub.yaml

.PHONY: help test-policy validate-envoy up down e2e e2e-stub build build-api build-mcp build-enricher local analyze tests-unit openapi

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

GIT_COMMIT   := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo unknown)
GIT_TAG      := $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
BUILD_DATE   := $(shell date -u +%Y-%m-%d)
GO_MODULE    := github.com/twistingmercury/mnemonic

INTEGRATION_PKGS := ./internal/api/repository/... ./internal/api/service/... \
	./internal/mcp/repository/... ./internal/enricher/repository/...

# The scripts build the image and then run its end-to-end suite in containers;
# they never push and never use the build cache.
build: build-api build-mcp build-enricher ## Build the api, mcp and enricher images

build-api: ## Build the api image and run its e2e suite
	build/api/api.build.sh

build-mcp: ## Build the mcp image and run its e2e suite
	build/mcp/mcp.build.sh

build-enricher: ## Build the enricher image and run its e2e suite
	build/enricher/enricher.build.sh

local: ## Build the api, mcp and enricher binaries into src/.bin (no tests, no Docker)
	mkdir -p .bin
	for svc in api mcp enricher; do \
		go -C src build -ldflags "-s -w \
			-X '$(GO_MODULE)/internal/$${svc}/version.version=$(GIT_TAG)' \
			-X '$(GO_MODULE)/internal/$${svc}/version.buildDate=$(BUILD_DATE)' \
			-X '$(GO_MODULE)/internal/$${svc}/version.commit=$(GIT_COMMIT)'" \
			-o .bin/mnemonic-$${svc} ./cmd/$${svc} || exit 1; \
	done

analyze: ## Run linters, formatters, security scanners on production code (no tests/ or *_test.go)
	@goimports -w src/cmd src/internal
	@cd src && golangci-lint run --tests=false ./...
	@cd src && govulncheck ./cmd/... ./internal/...
	@cd src && gosec -quiet ./...

tests-unit: ## Run the Go unit tests; coverage goes to src/.bin/coverage.out
	mkdir -p .bin
	cd src && go test -count=1 ./... -coverprofile=.bin/coverage.out

# Same invocation as build/api/api.Dockerfile.
openapi: ## Regenerate the OpenAPI docs into src/docs/api/swagger
	cd src && swag init -g cmd/api/main.go -o docs/api/swagger --parseInternal
