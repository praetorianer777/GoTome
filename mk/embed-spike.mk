# The embedding runtime spike (#15). Nothing here is part of the gate.
# embed-assets and embed-reference download from outside and are run by the
# owner; spikes/embed/README.md says what each step does.

EMBED_SPIKE_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)
EMBED_SPIKE_GO = docker run --rm \
	-u $(shell id -u):$(shell id -g) \
	-v $(EMBED_SPIKE_ROOT):/work \
	-v $(EMBED_SPIKE_ROOT)/.cache/go:/cache \
	-e HOME=/tmp \
	-e GOCACHE=/cache/build \
	-e GOMODCACHE=/cache/mod \
	-e GOFLAGS=-buildvcs=false \
	-e GOTOOLCHAIN=local \
	-w /work/spikes/embed golang:1.27-bookworm

.PHONY: embed-assets
embed-assets: ## Download the model and ONNX Runtime for the embedding spike (owner)
	$(EMBED_SPIKE_ROOT)/spikes/embed/fetch.sh

.PHONY: embed-reference
embed-reference: ## Compute the embedding spike's reference vectors with sentence-transformers (owner)
	docker run --rm -u $(shell id -u):$(shell id -g) -e HOME=/tmp \
		-v $(EMBED_SPIKE_ROOT)/spikes/embed:/work -w /work python:3.12-slim-bookworm \
		sh -c 'pip install --quiet --user --index-url https://download.pytorch.org/whl/cpu torch \
			&& pip install --quiet --user sentence-transformers \
			&& python reference.py testdata/texts.json testdata/reference.json'

.PHONY: embed-spike-build
embed-spike-build:
	@mkdir -p $(EMBED_SPIKE_ROOT)/.cache/embed $(EMBED_SPIKE_ROOT)/.cache/go
	$(EMBED_SPIKE_GO) sh -c 'CGO_ENABLED=0 go build -o /work/.cache/embed/embed-go.new . \
		&& go build -tags ORT -o /work/.cache/embed/embed-ort.new .'
	@# Renames, so that a run still using an old binary keeps it.
	@mv $(EMBED_SPIKE_ROOT)/.cache/embed/embed-go.new $(EMBED_SPIKE_ROOT)/.cache/embed/embed-go
	@mv $(EMBED_SPIKE_ROOT)/.cache/embed/embed-ort.new $(EMBED_SPIKE_ROOT)/.cache/embed/embed-ort

.PHONY: embed-spike
embed-spike: embed-spike-build ## Run one step of the embedding spike: STEP=check|speed|books|quality
	$(EMBED_SPIKE_ROOT)/spikes/embed/run.sh $(STEP)

.PHONY: embed-spike-test
embed-spike-test: ## Run the embedding spike's own tests
	$(EMBED_SPIKE_GO) sh -c 'test -z "$$(gofmt -l .)" && go vet ./... && go test -race ./...'
