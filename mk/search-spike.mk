# The search engine spike (#14). Nothing here is part of the gate: a run at
# 50,000 books takes hours and tens of gigabytes. spikes/search/README.md
# says what each step does.

SEARCH_SPIKE_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
SEARCH_SPIKE_GO = docker run --rm \
	-u $(shell id -u):$(shell id -g) \
	-v $(SEARCH_SPIKE_ROOT):/work \
	-v $(SEARCH_SPIKE_ROOT)/.cache/corpus:/corpus \
	-v $(SEARCH_SPIKE_ROOT)/.cache/go:/cache \
	-e HOME=/tmp \
	-e GOCACHE=/cache/build \
	-e GOMODCACHE=/cache/mod \
	-e GOFLAGS=-buildvcs=false \
	-e GOTOOLCHAIN=local \
	-w /work/spikes/search golang:1.27-bookworm

.PHONY: search-spike-build
search-spike-build:
	@mkdir -p $(SEARCH_SPIKE_ROOT)/.cache/search-spike $(SEARCH_SPIKE_ROOT)/.cache/go
	$(SEARCH_SPIKE_GO) go build -o /work/.cache/search-spike/search.new .
	@# A rename, so that a run still using the old binary keeps it.
	@mv $(SEARCH_SPIKE_ROOT)/.cache/search-spike/search.new $(SEARCH_SPIKE_ROOT)/.cache/search-spike/search

.PHONY: search-spike-extract
search-spike-extract: search-spike-build ## Read the base corpus into chunks for the search spike
	$(SEARCH_SPIKE_GO) /work/.cache/search-spike/search extract

.PHONY: search-spike
search-spike: search-spike-build ## Measure one search engine: ENGINE=pgsearch|fts|bleve BOOKS=0|50000
	$(SEARCH_SPIKE_ROOT)/spikes/search/bench.sh $(ENGINE) $(BOOKS)

.PHONY: search-spike-report
search-spike-report: search-spike-build ## Print the search spike's results as Markdown
	@$(SEARCH_SPIKE_GO) /work/.cache/search-spike/search report

.PHONY: search-spike-test
search-spike-test: ## Run the search spike's own tests
	$(SEARCH_SPIKE_GO) sh -c 'test -z "$$(gofmt -l .)" && go vet ./... && go test -race ./...'
