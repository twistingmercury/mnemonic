.PHONY: help build build-api build-mcp build-enricher analyze tests-unit openapi

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

# The scripts build the image and then run its end-to-end suite in containers;
# they never push and never use the build cache.
build: build-api build-mcp build-enricher ## Build the api, mcp and enricher images

build-api: ## Build the api image and run its e2e suite
	build/api/api.build.sh

build-mcp: ## Build the mcp image and run its e2e suite
	build/mcp/mcp.build.sh

build-enricher: ## Build the enricher image and run its e2e suite
	build/enricher/enricher.build.sh

analyze: ## Run linters, formatters, security scanners on production code (no tests/ or *_test.go)
	@goimports -w src/cmd src/internal
	@cd src && golangci-lint run --tests=false ./...
	@cd src && govulncheck ./cmd/... ./internal/...
	@cd src && gosec -quiet ./...

tests-unit: ## Run the Go unit tests with the race detector
	go -C src test -race -count=1 ./...

# Same invocation as build/api/api.Dockerfile.
openapi: ## Regenerate the OpenAPI docs into src/docs/api/swagger
	cd src && swag init -g cmd/api/main.go -o docs/api/swagger --parseInternal
