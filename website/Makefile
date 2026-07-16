.DEFAULT_GOAL := help

.PHONY: help dev build serve clean install test typecheck

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	npm install

dev: ## Run documentation site locally
	npm run start

build: ## Build documentation site
	npm run build

serve: build ## Serve built documentation site locally
	npm run serve

test: ## Run website tests
	npm test

typecheck: ## Run TypeScript checks
	npm run typecheck

clean: ## Remove build artifacts
	rm -rf build .docusaurus .cache-loader
