.DEFAULT_GOAL := help

GO ?= go
MODULE_PATH := $(shell $(GO) list -m -f '{{.Path}}')
BUILD_OUT_DIR := $(CURDIR)/.out
SWOBU_VERSION ?= dev
SWOBU_LDFLAGS := -s -w -X $(MODULE_PATH)/internal/app/operator/controlplane.swobuVersion=$(SWOBU_VERSION)
GO_TEST_FLAGS ?= -failfast -timeout=5m

.PHONY: help verify test build artifacts clean fmt-check lint

help: ## Show OSS entrypoints
	@awk 'BEGIN {FS = ":.*## "; print "swobucli/oss entrypoints:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

verify: ## Run OSS local quality gate
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test

test: ## Run deterministic required tests
	CGO_ENABLED=0 $(GO) test $(GO_TEST_FLAGS) ./...

build: ## Build local swobu binary artifact
	@mkdir -p $(BUILD_OUT_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(SWOBU_LDFLAGS)" -o $(BUILD_OUT_DIR)/swobu ./cmd/swobu

artifacts: ## Build release archives + checksums into dist/release/v<SWOBU_VERSION>
	./scripts/release.sh "$(SWOBU_VERSION)"

clean: ## Remove local generated build outputs
	rm -rf .out dist

fmt-check:
	@set -eu; \
	gofmt_out="$$(find cmd internal -type f -name '*.go' -print0 | xargs -0r gofmt -l)"; \
	if [ -n "$$gofmt_out" ]; then \
		printf 'Files need formatting:\n%s\n' "$$gofmt_out"; \
		exit 1; \
	fi

lint:
	CGO_ENABLED=0 $(GO) build ./...
	CGO_ENABLED=0 $(GO) vet ./...
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/rolelint ../opencore/internal/...
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/codelint ../opencore/internal/...
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/trimlowerlint ../oss/internal/...
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/viewgrammarlint ../opencore/internal/terminalui
