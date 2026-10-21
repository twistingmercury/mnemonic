.DEFAULT_GOAL := help

.PHONY: help test start stop analyze

COMPOSE := docker compose -f docker-compose.yaml

test: ## Start the stack, run the suite, then tear everything down
	@$(COMPOSE) up --build --abort-on-container-exit --exit-code-from librarian_tests; \
		status=$$?; $(MAKE) --no-print-directory stop; exit $$status

start: ## Start the stack without running tests (for local investigation)
	$(COMPOSE) up --build -d --wait librarian_api librarian_mcp librarian_enricher

stop: ## Tear down all containers and volumes
	@$(COMPOSE) down -v --remove-orphans > /dev/null 2>&1 || true

analyze: ## Lint all tracked Markdown files
	markdownlint -c .markdownlint.json $$(git ls-files '*.md')

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
