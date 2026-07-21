.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

GO ?= go
MODULE_PATH := $(shell $(GO) list -m -f '{{.Path}}')
BUILD_OUT_DIR := $(CURDIR)/.out
SWOBU_VERSION ?= dev
SWOBU_LDFLAGS := -s -w -X $(MODULE_PATH)/internal/app/operator/controlplane.swobuVersion=$(SWOBU_VERSION)
GO_TEST_FLAGS ?= -failfast -timeout=5m

.PHONY: help check check-fmt check-test test generate build artifacts clean fmt-check lint audit

help: ## Show public entrypoints
	@awk 'BEGIN {FS = ":.*## "; print "swobucli/opencore entrypoints:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: ## Run all child checks
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test

check-fmt: ## Run child formatting check
	@$(MAKE) fmt-check

check-test: ## Run child tests
	@$(MAKE) test

generate: ## Regenerate cockpit GSX sources
	@$(GO) generate ./internal/cockpit

# Cockpit .gsx sources are the source of truth, so every compile path refreshes
# generated Go before building or packaging anything that imports cockpit.
build: generate ## Build local swobu binary artifact
	@mkdir -p $(BUILD_OUT_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(SWOBU_LDFLAGS)" -o $(BUILD_OUT_DIR)/swobu ./cmd/swobu

artifacts: generate ## Build release archives + checksums into dist/release/v<SWOBU_VERSION>
	./scripts/release.sh "$(SWOBU_VERSION)"

test: generate
	@CGO_ENABLED=0 $(GO) test $(GO_TEST_FLAGS) ./...

fmt-check:
	@set -eu; \
	gofmt_out="$$(find cmd internal -type f -name '*.go' -print0 | xargs -0r gofmt -l)"; \
	if [ -n "$$gofmt_out" ]; then \
		printf 'Files need formatting:\n%s\n' "$$gofmt_out"; \
		exit 1; \
	fi

lint:
	@CGO_ENABLED=0 $(GO) build ./...
	@CGO_ENABLED=0 $(GO) vet ./...
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/check-opencore-lint

audit: ## Run advisory whole-tree naming, structure, provenance, and clone diagnostics
	@cd ../tools && CGO_ENABLED=0 $(GO) run ./cmd/check-opencore-lint --audit

clean:
	rm -rf .out dist
