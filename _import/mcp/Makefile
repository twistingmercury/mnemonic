ROOT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
SRC_DIR := $(ROOT_DIR)src

GIT_COMMIT := $(shell git -C "$(ROOT_DIR)" rev-parse --short=8 HEAD 2>/dev/null || echo unknown)
GIT_TAG := $(shell git -C "$(ROOT_DIR)" describe --tags --abbrev=0 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u +%Y-%m-%d)
LD_FLAGS := -s -w \
	-X 'github.com/twistingmercury/librarian-mcp/internal/version.version=$(GIT_TAG)' \
	-X 'github.com/twistingmercury/librarian-mcp/internal/version.buildDate=$(BUILD_DATE)' \
	-X 'github.com/twistingmercury/librarian-mcp/internal/version.commit=$(GIT_COMMIT)'

.DEFAULT_GOAL := help

.PHONY: help local build start stop analyze tests-unit tests-coverage tests-bench

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

local: ## Build the mnemonic binary locally
	@mkdir -p "$(SRC_DIR)/.bin"
	go -C "$(SRC_DIR)" build -ldflags "$(LD_FLAGS)" -o .bin/mnemonic cmd/main/main.go

build: ## Build the Docker image and run end-to-end tests
	LOCAL=1 "$(SRC_DIR)/build/build.sh"

start: ## Start the local development stack
	@printf "Starting mnemonic..."
	@docker compose -f "$(ROOT_DIR)docker-compose.yaml" up -d
	@printf "done\n"

stop: ## Stop the local development stack and remove its volumes
	@printf "Stopping mnemonic..."
	@docker compose -f "$(ROOT_DIR)docker-compose.yaml" down -v --remove-orphans
	@printf "done\n"

analyze: ## Run formatters, linters, and security scanners
	cd "$(SRC_DIR)" && goimports -w .
	cd "$(SRC_DIR)" && golangci-lint run
	cd "$(SRC_DIR)" && govulncheck ./cmd/... ./internal/...
	cd "$(SRC_DIR)" && gosec -quiet -exclude-dir=tests ./...

tests-unit: ## Run all Go module unit tests
	go -C "$(SRC_DIR)" test ./...

tests-coverage: ## Generate HTML unit-test coverage at src/.bin/coverage.html
	@mkdir -p "$(SRC_DIR)/.bin"
	go -C "$(SRC_DIR)" test ./... -coverprofile=.bin/coverage.out
	go -C "$(SRC_DIR)" tool cover -html=.bin/coverage.out -o .bin/coverage.html

tests-bench: ## Run all Go module benchmarks
	go -C "$(SRC_DIR)" test ./... -bench=. -benchmem -run=^$$
