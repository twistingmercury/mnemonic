.DEFAULT_GOAL := help

.PHONY: help analyze

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nAvailable targets:\n"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

analyze: ## Lint all tracked Markdown files
	markdownlint -c .markdownlint.json $$(git ls-files '*.md')
