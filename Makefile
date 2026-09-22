.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

GO ?= go
export GOWORK := off
export GOENV := off
export GOFLAGS :=
MODULE_PATH := github.com/swobuforge/swobu
BUILD_OUT_DIR := $(CURDIR)/.out
SWOBU_VERSION ?= dev
SWOBU_LDFLAGS := -s -w -X $(MODULE_PATH)/internal/app/operator/controlplane.swobuVersion=$(SWOBU_VERSION)
GO_TEST_FLAGS ?= -failfast -timeout=5m

.PHONY: help check check-build check-fixture-line-endings check-fmt check-installer check-test test generate check-generated build release-build clean fmt-check lint

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; print "Swobu commands:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: check-build check-generated check-installer check-fixture-line-endings ## Run all checks
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

generate: ## Regenerate cockpit GSX sources
	@$(GO) generate ./internal/cockpit

check-generated: ## Check generated source without changing the checkout
	@sh ./scripts/check-generated.sh

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
	gofmt_out="$$(find cmd internal shareprotocol scripts -type f -name '*.go' -print0 | xargs -0 gofmt -l)"; \
	if [ -n "$$gofmt_out" ]; then \
		printf 'Files need formatting:\n%s\n' "$$gofmt_out"; \
		exit 1; \
	fi

lint:
	@CGO_ENABLED=0 $(GO) build -mod=readonly ./...
	@CGO_ENABLED=0 $(GO) vet -mod=readonly ./...

clean:
	rm -rf .out dist
