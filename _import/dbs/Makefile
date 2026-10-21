.PHONY: help build build-postgres build-neo4j test test-postgres test-neo4j

default: help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: build-postgres build-neo4j ## Build both database images

build-postgres: ## Build the mnemonic-postgres image
	bash src/postgres/build.sh

build-neo4j: ## Build the mnemonic-neo4j image
	bash src/neo4j/build.sh

test: test-postgres test-neo4j ## Run all database tests

test-postgres: ## Run PostgreSQL migration BATS tests
	cd src && make tests-postgres

test-neo4j: ## Run Neo4j migration BATS tests
	cd src && make tests-neo4j

