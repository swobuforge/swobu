.DEFAULT_GOAL := help

.PHONY: help verify test run check-entrypoints

help: ## Show project-scoped targets
	@$(MAKE) -f Makefile.core help

check-entrypoints: ## Verify monorepo project entrypoint contract
	@./scripts/checks/project-entrypoints.sh

verify: check-entrypoints ## Run swobucli merge-safety gate
	@$(MAKE) -f Makefile.core verify

test: ## Run swobucli deterministic tests
	@$(MAKE) -f Makefile.core test

run: ## Run swobucli operator surface
	@$(MAKE) -f Makefile.core run

# Forward any non-public target to Makefile.core so recursive `make <target>`
# calls inside core recipes keep working when this wrapper is the default file.
%:
	@$(MAKE) -f Makefile.core $@
