# ecctl-e2e developer targets. Run from the e2e/ directory.
BIN := bin/ecctl-e2e
PUBLIC_ECCTL := bin/ecctl-public
FULL_ECCTL := bin/ecctl-full
REPORTS := reports

.DEFAULT_GOAL := help
.PHONY: help build build-public build-full run run-public run-full coverage coverage-full sweep sweep-public sweep-full list validate lint test fmt

help: ## Show this help
	@echo "ecctl-e2e — make targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: build-public ## Build the public E2E runner and default public ecctl binary

build-public: ## Build ecctl-e2e and the default public ecctl binary into bin/
	go build -o $(BIN) ./cmd/ecctl-e2e
	cd .. && go build -o e2e/$(PUBLIC_ECCTL) ./cmd/ecctl

build-full: ## Build ecctl-e2e and the full-surface ecctl binary into bin/
	go build -o $(BIN) ./cmd/ecctl-e2e
	cd .. && go build -o e2e/$(FULL_ECCTL) ./cmd/ecctl-full

run: run-public ## Run the public E2E surface against cloud

run-public: build-public lint ## Real public-surface run (needs ALIBABA_CLOUD_* credentials or an aliyun profile)
	$(BIN) run --surface public --ecctl-bin $(PUBLIC_ECCTL) --report-dir $(REPORTS)

run-full: build-full lint ## Real full-surface run for hidden resources
	$(BIN) run --surface full --ecctl-bin $(FULL_ECCTL) --report-dir $(REPORTS)

coverage: build-public ## Enforce the public capability registry is fully live-pass
	$(BIN) coverage registry check --specs ../specs --cases cases --registry coverage.yaml --ecctl-bin $(PUBLIC_ECCTL) --surface public --fail-on-missing --fail-on-not-live

coverage-full: build-full ## Enforce the full capability registry is fully live-pass
	$(BIN) coverage registry check --specs ../specs --cases cases --registry coverage.yaml --ecctl-bin $(FULL_ECCTL) --surface full --fail-on-missing --fail-on-not-live

sweep: sweep-public ## Sweep leftovers created by public-surface cases

sweep-public: build-public ## Clean up public-surface leftovers in the cloud
	$(BIN) sweep --ecctl-bin $(PUBLIC_ECCTL)

sweep-full: build-full ## Clean up full-surface leftovers in the cloud
	$(BIN) sweep --ecctl-bin $(FULL_ECCTL)

list: build-public ## List public-surface cases
	$(BIN) run --surface public --collect-only

validate: lint ## Validate public case YAML offline (including stack contracts)
	$(BIN) run --surface public --collect-only -q >/dev/null

lint: build-public ## Validate cases, stack dependencies, run config, and registry links offline
	$(BIN) lint cases --config e2e.yaml --cases cases --stack fixtures/stack.yaml --coverage coverage.yaml --parameter-policy fixtures/parameter-policy.yaml

test: ## Run unit tests
	go test ./...

fmt: ## Format Go sources
	gofmt -w .
