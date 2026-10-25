.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

GO ?= go
export GOWORK := off
export GOENV := off
export GOFLAGS := -buildvcs=false
MODULE_PATH := github.com/swobuforge/swobu
BUILD_OUT_DIR := $(CURDIR)/.out
SWOBU_VERSION ?= dev
SWOBU_LDFLAGS := -s -w -X $(MODULE_PATH)/internal/app/operator/controlplane.swobuVersion=$(SWOBU_VERSION)
GO_TEST_FLAGS ?= -failfast -timeout=5m

.PHONY: help check check-build check-fixture-line-endings check-fmt check-installer check-test test build release-build clean fmt-check lint

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; print "Swobu commands:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: check-build check-openapi check-api-inventory check-installer check-fixture-line-endings ## Run all checks
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test

check-fmt: ## Check formatting
	@$(MAKE) fmt-check

check-test: ## Run tests
	@$(MAKE) test

check-installer: ## Check installer argument handling
	@DRY_RUN=true START_SWOBU=false ./scripts/install.sh --version swobu-v0.0.0 >/dev/null

check-fixture-line-endings:
	@sh ./scripts/check-fixture-line-endings.sh

.PHONY: openapi-generate check-openapi
openapi-generate: ## Generate the canonical target OpenAPI skeleton
	@$(GO) run -mod=readonly -buildvcs=false ./scripts/openapi

check-openapi: ## Check target OpenAPI drift without changing files
	@$(GO) run -mod=readonly -buildvcs=false ./scripts/openapi -check

.PHONY: api-inventory-generate check-api-inventory
api-inventory-generate: ## Capture reviewed HTTP registrations and legacy consumers
	@$(GO) run -mod=readonly -buildvcs=false ./scripts/api-inventory

check-api-inventory: ## Reject unreviewed route or legacy consumer drift
	@$(GO) run -mod=readonly -buildvcs=false ./scripts/api-inventory -check

build: ## Build local swobu binary artifact
	@mkdir -p $(BUILD_OUT_DIR)
	CGO_ENABLED=0 $(GO) build -mod=readonly -trimpath -buildvcs=false -ldflags "$(SWOBU_LDFLAGS)" -o $(BUILD_OUT_DIR)/swobu ./cmd/swobu

check-build:
	@temp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$temp_dir"' EXIT; \
	CGO_ENABLED=0 $(GO) build -mod=readonly -trimpath -buildvcs=false -ldflags "$(SWOBU_LDFLAGS)" -o "$$temp_dir/swobu" ./cmd/swobu

release-build: ## Build reproducible release artifacts from explicit source inputs
	@sh ./scripts/build-release.sh "$(SWOBU_VERSION)" "$(SOURCE_COMMIT)" "$(SOURCE_TREE)" "$(RELEASE_OUTPUT)"

test:
	@CGO_ENABLED=0 $(GO) test -mod=readonly $(GO_TEST_FLAGS) ./...

fmt-check:
	@set -eu; \
	gofmt_out="$$(find cmd internal relayprotocol scripts -type f -name '*.go' -print0 | xargs -0 gofmt -l)"; \
	if [ -n "$$gofmt_out" ]; then \
		printf 'Files need formatting:\n%s\n' "$$gofmt_out"; \
		exit 1; \
	fi

lint:
	@CGO_ENABLED=0 $(GO) build -mod=readonly ./...
	@CGO_ENABLED=0 $(GO) vet -mod=readonly ./...

clean:
	rm -rf .out dist

.PHONY: webui-generate webui-verify webui-test webui-e2e webui-visual
webui-generate: ## Generate client and staged Web bundle with source manifest
	@cd webui && bun scripts/build.ts
webui-verify: ## Rebuild Web output in temp and reject staged drift
	@cd webui && bun scripts/build.ts --check
webui-test: ## Check Web types, lint and unit contracts
	@cd webui && bun run typecheck && bun run lint && bun run test
webui-e2e: webui-verify ## Run staged production Web browser journeys
	@cd webui && bun run e2e
webui-visual: webui-verify ## Capture accepted-shell comparison candidates
	@cd webui && bun run e2e tests/e2e/reproduce.spec.ts

.PHONY: webui-check-embed
webui-check-embed: webui-verify ## Compile staged assets as Go embed without production cutover
	@cd webui && bun scripts/check-embed.ts

# W01 source-to-embed publication: staging/verification must succeed before
# replacing the production Go embed asset set. This is a build-time operation.
.PHONY: webui-cutover
webui-cutover: webui-check-embed ## Publish verified Web Operator bytes into Go embed
	@cd webui && bun scripts/install-embed.ts

.PHONY: webui-install
webui-install: ## Install pinned Web dependencies without lock mutation
	@cd webui && bun install --frozen-lockfile
