.DEFAULT_GOAL := help

.PHONY: help tests tests-postgres tests-neo4j

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

tests-postgres: ## Run PostgreSQL migration BATS tests
	./tests/postgres-test-runner.sh

tests-neo4j: ## Run Neo4j migration BATS tests
	./tests/neo4j-test-runner.sh

tests: tests-postgres tests-neo4j ## Run all migration BATS tests
