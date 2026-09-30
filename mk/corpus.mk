# The benchmark corpus for the search and embedding spikes. Nothing here is part
# of the gate: fetching takes over an hour, and the 50k corpus is 25 times the
# base corpus on disk.

# Set here as well as in the Makefile, so the targets also work with
# make -f mk/corpus.mk from the checkout root.
CORPUS_ROOT  := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
CORPUS_DIR   ?= $(CORPUS_ROOT)/.cache/corpus
CORPUS_LANGS ?= en:1200,de:600,fr:200
CORPUS_COUNT ?= 50000
CORPUS_GO_IMAGE ?= golang:1.27-bookworm

CORPUS_GO = docker run --rm \
	-u $(shell id -u):$(shell id -g) \
	-v $(CORPUS_ROOT):/work \
	-v $(CORPUS_DIR):/corpus \
	-v $(CORPUS_ROOT)/.cache/go:/cache \
	-e HOME=/tmp \
	-e GOCACHE=/cache/build \
	-e GOMODCACHE=/cache/mod \
	-e GOFLAGS=-buildvcs=false \
	-e GOTOOLCHAIN=local \
	-w /work/spikes/corpus $(CORPUS_GO_IMAGE)

# One target of its own rather than a rule per directory: mk/go.mk already has
# the rule for .cache/go, and make warns when two files define it.
.PHONY: corpus-dirs
corpus-dirs:
	@mkdir -p $(CORPUS_DIR) $(CORPUS_ROOT)/.cache/go

.PHONY: corpus
corpus: | corpus-dirs ## Download the base benchmark corpus (about 2,000 EPUBs, over an hour)
	$(CORPUS_GO) go run . fetch -dir /corpus/base -langs $(CORPUS_LANGS)

.PHONY: corpus-50k
corpus-50k: | corpus-dirs ## Replicate the base corpus to 50,000 books (25 times its size on disk)
	$(CORPUS_GO) go run . replicate -from /corpus/base -dir /corpus/50k -count $(CORPUS_COUNT)

.PHONY: corpus-stats
corpus-stats: | corpus-dirs ## Print the composition of the base corpus
	$(CORPUS_GO) go run . stats -dir /corpus/base

.PHONY: corpus-test
corpus-test: | corpus-dirs ## Run the corpus tool's own tests
	$(CORPUS_GO) sh -c 'test -z "$$(gofmt -l .)" && go vet ./... && go test -race ./...'
