.DEFAULT_GOAL := help

GO ?= go
MODULE_PATH := $(shell $(GO) list -m -f '{{.Path}}')
BUILD_OUT_DIR := $(CURDIR)/.out
SWOBU_VERSION ?= dev
SWOBU_LDFLAGS := -s -w -X $(MODULE_PATH)/internal/app/operator/controlplane.swobuVersion=$(SWOBU_VERSION)
GO_TEST_FLAGS ?= -failfast -timeout=5m

.PHONY: help verify test run build verify-published publish release patch minor major clean

help: ## Show OSS entrypoints
	@awk 'BEGIN {FS = ":.*## "; print "swobucli/oss entrypoints:"} /^[a-zA-Z0-9_.-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

verify: ## Run merge-safety gate
	@$(MAKE) fmt-check
	@$(MAKE) lint
	@$(MAKE) test

test: ## Run deterministic required tests
	CGO_ENABLED=0 $(GO) test $(GO_TEST_FLAGS) ./...

run: ## Run swobu operator surface
	@if [ ! -t 1 ]; then \
		echo "run target requires interactive terminal; skipping in non-interactive shell"; \
	else \
		$(GO) run ./cmd/swobu; \
	fi

build: ## Build local swobu binary artifact
	@mkdir -p $(BUILD_OUT_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(SWOBU_LDFLAGS)" -o $(BUILD_OUT_DIR)/swobu ./cmd/swobu

verify-published: ## Verify published latest release and installer reachability
	./scripts/checks/verify-published.sh

publish: ## Create and push release tag: make publish patch|minor|major
	@kind="$(filter patch minor major,$(MAKECMDGOALS))"; \
	if [ -z "$$kind" ]; then \
		echo "usage: make publish patch|minor|major" >&2; \
		exit 1; \
	fi; \
	./scripts/release.sh "$$kind"

release: publish

patch minor major:
	@:

clean: ## Remove local generated build outputs
	rm -rf .out dist

fmt-check:
	@set -eu; \
	files="$$(find cmd internal test -type f -name '*.go' 2>/dev/null)"; \
	if [ -z "$$files" ]; then \
		exit 0; \
	fi; \
	gofmt_out="$$(gofmt -l $$files)"; \
	if [ -n "$$gofmt_out" ]; then \
		printf 'Files need formatting:\n%s\n' "$$gofmt_out"; \
		exit 1; \
	fi

lint:
	CGO_ENABLED=0 $(GO) build ./...
