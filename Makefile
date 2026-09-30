.DEFAULT_GOAL := help

.PHONY: help test

help:
	@grep -E '^[a-z][a-z-]*:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

test: ## Run the push gate
	./run-tests.sh
