.PHONY: local build analyze tests-unit tests-integration tests-bench docs-swagger start stop help

GIT_COMMIT := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_TAG    := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%d)

default: help

build: ## Build Docker image locally using the full CI build script
	cd src && LOCAL=1 ./build/build.sh

analyze: ## Run linters, formatters, security scanners, etc
	cd src && goimports -w .
	cd src && golangci-lint run
	cd src && govulncheck ./cmd/... ./internal/...
	cd src && gosec -quiet -exclude-dir=tests ./...

tests-unit: ## Run unit tests with coverage
	cd src && go test ./internal/... -coverprofile=coverage.out
	cd src && go tool cover -html=coverage.out

tests-integration: ## Run integration tests against a throwaway PostgreSQL (requires Docker)
	./src/tests/db/run.sh $(PKGS)

tests-bench: ## Run benchmark tests
	cd src && go test ./internal/... -bench=. -benchmem -run=^$$

docs-swagger: ## Generate Swagger 2.0 docs
	cd src && go install github.com/swaggo/swag/cmd/swag@v1.16.6
	cd src && swag init -g cmd/main/main.go -o docs/swagger --parseInternal

start: ## Start mnemonic using the latest image via Docker Compose
	@printf "Starting mnemonic..."
	@docker compose -f ./docker-compose.yaml up -d
	@printf "done\n"

stop: ## Tear down mnemonic infrastructure started with 'make start'
	@printf "Stopping mnemonic.."
	@docker compose down -v --remove-orphans > /dev/null 2>&1 || true
	@docker rmi migrate/migrate:latest -f > /dev/null 2>&1 || true
	@docker system prune -v > /dev/null 2>&1 || true
	@printf "done\n"

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
