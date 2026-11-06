TEST_PACKAGES := ./internal/... ./pkg/... ./cmd/... ./specs/...
REVIEW_CACHE_DIR ?= $(patsubst %/,%,$(if $(TMPDIR),$(TMPDIR),/tmp))/ecctl-review-$(notdir $(CURDIR))
REVIEW_GOPATH ?= $(REVIEW_CACHE_DIR)/go
REVIEW_GOCACHE ?= $(REVIEW_CACHE_DIR)/go-build
REVIEW_GOMODCACHE ?= $(REVIEW_CACHE_DIR)/go-mod
REVIEW_NPM_CACHE ?= $(REVIEW_CACHE_DIR)/npm
REVIEW_GOPROXY ?= https://proxy.golang.org,direct
REVIEW_ENV = env GOPATH="$(REVIEW_GOPATH)" GOCACHE="$(REVIEW_GOCACHE)" GOMODCACHE="$(REVIEW_GOMODCACHE)" GOPROXY="$(REVIEW_GOPROXY)" npm_config_cache="$(REVIEW_NPM_CACHE)"

.PHONY: help install build test coverage ci-test lint fmt clean generate metadata-prepare metadata-check metadata-sync drift drift-baseline drift-check specdrift prepare-public-release check-public-release check-release-version review-final review-dsh-plugin review-e2e review-website

install: ## Install git pre-commit hook
	git config core.hooksPath .githooks

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "; printf "Available targets:\n"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[1;36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: metadata-check ## Build bin/ecctl from prepared metadata
	@mkdir -p bin
	go build -o bin/ecctl ./cmd/ecctl
	@printf '\033[1;32mbuilt bin/ecctl\033[0m (%s, %s bytes)\n' "$$(./bin/ecctl --version 2>/dev/null | head -1)" "$$(wc -c < bin/ecctl | tr -d ' ')"

test: metadata-check ## Run all Go tests offline after metadata preparation
	go test $(TEST_PACKAGES)

coverage: metadata-check ## Run Go tests and require at least 95% statement coverage
	@mkdir -p reports
	@set -eu; \
	rm -f reports/coverage.out; \
	pkgs=$$(go list $(TEST_PACKAGES)); \
	pkgs=$$(printf '%s\n' "$$pkgs" | grep -v '/cmd/specgen$$'); \
	coverpkgs=$$(printf '%s\n' "$$pkgs" | paste -sd, -); \
	go test -coverpkg="$$coverpkgs" -coverprofile=reports/coverage.out $$pkgs; \
	awk -f .github/scripts/check-coverage.awk reports/coverage.out

ci-test: metadata-check ## Run CI tests and write reports
	@mkdir -p reports
	@set -eu; \
	rm -f reports/coverage.out reports/coverage.xml reports/testcase.xml; \
	pkgs=$$(go list $(TEST_PACKAGES)); \
	pkgs=$$(printf '%s\n' "$$pkgs" | grep -v '/cmd/specgen$$'); \
	coverpkgs=$$(printf '%s\n' "$$pkgs" | paste -sd, -); \
	status=0; \
	go run gotest.tools/gotestsum@v1.13.0 --junitfile reports/testcase.xml -- -coverpkg="$$coverpkgs" -coverprofile=reports/coverage.out $$pkgs || status=$$?; \
	if [ -f reports/coverage.out ]; then \
		go tool cover -func=reports/coverage.out | awk '/^total:/ {print}'; \
		go run github.com/boumenot/gocover-cobertura@v1.5.0 -strict < reports/coverage.out > reports/coverage.xml; \
	fi; \
	test "$$status" -eq 0; \
	awk -f .github/scripts/check-coverage.awk reports/coverage.out

generate: ## Generate Go catalog from resource specs
	go run ./cmd/specgen -spec-dir specs -out pkg/spec/catalog_generated.go

# Metadata tools run on the host even when ecctl is cross-compiled.
metadata-prepare: ## Prepare ignored metadata from the lock; download only when needed
	GOOS="$$(go env GOHOSTOS)" GOARCH="$$(go env GOHOSTARCH)" go run ./cmd/openapimeta-sync -prepare -archive "$(METADATA_ARCHIVE)" -out internal/openapimeta

metadata-check: ## Verify all prepared metadata offline without changing the lock
	GOOS="$$(go env GOHOSTOS)" GOARCH="$$(go env GOHOSTARCH)" go run ./cmd/openapimeta-sync -check -out internal/openapimeta

metadata-sync: ## Explicitly update the metadata lock from a local archive and full revision
	@test -n "$(METADATA_ARCHIVE)" || (echo "METADATA_ARCHIVE is required (downloaded tar.gz)" >&2; exit 1)
	@test -n "$(METADATA_REVISION)" || (echo "METADATA_REVISION is required (full 40-hex commit SHA)" >&2; exit 1)
	GOOS="$$(go env GOHOSTOS)" GOARCH="$$(go env GOHOSTARCH)" go run ./cmd/openapimeta-sync -archive "$(METADATA_ARCHIVE)" -revision "$(METADATA_REVISION)" -out internal/openapimeta

drift: specdrift ## Report OpenAPI metadata changes unmatched by resource bindings
	bin/specdrift detect -spec-dir specs

drift-baseline: specdrift ## Record the current OpenAPI metadata as the drift baseline
	bin/specdrift baseline -spec-dir specs

drift-check: specdrift ## Fail on unacknowledged OpenAPI metadata or binding coverage drift
	bin/specdrift detect -spec-dir specs -check

specdrift: metadata-check ## Build the specdrift CLI into bin/
	@mkdir -p bin
	go build -o bin/specdrift ./cmd/specdrift

prepare-public-release: ## Freeze PUBLIC_MODULE into module path, imports, and docs
	@test -n "$(PUBLIC_MODULE)" || (echo "PUBLIC_MODULE is required, for example github.com/<owner>/ecctl" >&2; exit 1)
	go run ./cmd/releaseprep --write --module "$(PUBLIC_MODULE)"

check-public-release: ## Check public release readiness gates
	go run ./cmd/releaseprep --check --repository "$${GITHUB_REPOSITORY:-aliyun/elastic-compute-control-cli}"

check-release-version: ## Check the canonical release version file
	go run ./cmd/releaseprep --check-version-file --version-file version.txt

lint: metadata-check specdrift ## Run formatting, vet, generated-code, and offline spec drift checks
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './bin/*'))"
	go vet ./...
	go run ./cmd/specgen -spec-dir specs -out pkg/spec/catalog_generated.go -check
	bin/specdrift detect -spec-dir specs -check

review-final: ## Run the complete offline review gates once for a final candidate SHA
	@mkdir -p "$(REVIEW_GOPATH)" "$(REVIEW_GOCACHE)" "$(REVIEW_GOMODCACHE)" "$(REVIEW_NPM_CACHE)"
	@printf 'review cache: %s\n' "$(REVIEW_CACHE_DIR)"
	$(REVIEW_ENV) $(MAKE) lint
	$(REVIEW_ENV) $(MAKE) test
	$(REVIEW_ENV) $(MAKE) review-dsh-plugin
	$(REVIEW_ENV) $(MAKE) review-e2e
	$(REVIEW_ENV) $(MAKE) review-website

review-dsh-plugin: ## Install, test, and inspect the DSH plugin package
	npm --prefix dsh-plugin ci --ignore-scripts --registry=https://registry.npmjs.org
	npm --prefix dsh-plugin test
	npm pack ./dsh-plugin --dry-run

review-e2e: ## Run E2E unit tests and offline public-surface gates
	$(MAKE) -C e2e test
	$(MAKE) -C e2e lint
	e2e/bin/ecctl-e2e coverage registry check \
		--specs specs \
		--cases e2e/cases \
		--registry e2e/coverage.yaml \
		--ecctl-bin e2e/bin/ecctl-public \
		--surface public
	e2e/bin/ecctl-e2e sweep check \
		--cases e2e/cases \
		--config e2e/sweep.yaml

review-website: ## Install and run website tests, typecheck, and production build
	npm --prefix website ci
	npm --prefix website run test
	npm --prefix website run typecheck
	npm --prefix website run build

fmt: ## Format Go packages
	go fmt ./...

clean: ## Remove build artifacts and test cache; retain downloaded metadata sources
	rm -rf bin reports internal/openapimeta/.generated
	go clean -testcache
