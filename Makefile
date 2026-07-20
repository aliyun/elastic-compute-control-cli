TEST_PACKAGES := ./pkg/... ./cmd/... ./specs/...

.PHONY: help install build test coverage ci-test lint fmt clean generate prepare-public-release check-public-release

install: ## Install git pre-commit hook
	git config core.hooksPath .githooks

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Available targets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[1;36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build bin/ecctl
	@mkdir -p bin
	go build -o bin/ecctl ./cmd/ecctl
	@printf '\033[1;32mbuilt bin/ecctl\033[0m (%s, %s bytes)\n' "$$(./bin/ecctl --version 2>/dev/null | head -1)" "$$(wc -c < bin/ecctl | tr -d ' ')"

test: ## Run all Go tests
	go test $(TEST_PACKAGES)

coverage: ## Run Go tests with total coverage
	@coverage=$$(mktemp); \
	trap 'rm -f "$$coverage"' EXIT; \
	pkgs=$$(go list $(TEST_PACKAGES) | grep -v '/cmd/specgen$$'); \
	go test -coverprofile="$$coverage" $$pkgs && \
	go tool cover -func="$$coverage" | awk '/^total:/ {print}'

ci-test: ## Run CI tests and write reports
	@mkdir -p reports
	@set +e; \
	pkgs=$$(go list $(TEST_PACKAGES) | grep -v '/cmd/specgen$$'); \
	go run gotest.tools/gotestsum@v1.13.0 --junitfile reports/testcase.xml -- -coverprofile=reports/coverage.out $$pkgs; \
	status=$$?; \
	set -e; \
	if [ -f reports/coverage.out ]; then \
		go tool cover -func=reports/coverage.out | awk '/^total:/ {print}'; \
		go run github.com/boumenot/gocover-cobertura@v1.5.0 < reports/coverage.out > reports/coverage.xml; \
	fi; \
	exit $$status

generate: ## Generate Go catalog from resource specs
	go run ./cmd/specgen -spec-dir specs -out pkg/spec/catalog_generated.go

prepare-public-release: ## Freeze PUBLIC_MODULE into module path, imports, and docs
	@test -n "$(PUBLIC_MODULE)" || (echo "PUBLIC_MODULE is required, for example github.com/<owner>/ecctl" >&2; exit 1)
	go run ./cmd/releaseprep --write --module "$(PUBLIC_MODULE)"

check-public-release: ## Check public release readiness gates
	go run ./cmd/releaseprep --check --repository "$${GITHUB_REPOSITORY:-aliyun/elastic-compute-control-cli}"

lint: ## Run formatting, vet, and generated-code checks
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './bin/*'))"
	go vet ./...
	go run ./cmd/specgen -spec-dir specs -out pkg/spec/catalog_generated.go -check

fmt: ## Format Go packages
	go fmt ./...

clean: ## Remove build artifacts and test cache
	rm -rf bin reports
	go clean -testcache
