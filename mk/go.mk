# The Go backend. Every target runs the toolchain in a container as the invoking
# user, with module and build caches under .cache/ so they survive between runs.

# Bookworm rather than alpine: the race detector needs cgo, and alpine ships no
# C compiler. The version is pinned; see CLAUDE.md.
GO_IMAGE ?= golang:1.27-bookworm
GO_CACHE := $(ROOT)/.cache/go
GO_PKG   := github.com/praetorianer777/gotome/backend
GO_LDFLAGS := -X $(GO_PKG)/internal/version.Version=$(VERSION)

# The argument is extra docker options: the integration suite adds the stack's
# network and the database URL (mk/stack.mk).
go_run = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-v $(GO_CACHE):/cache \
	-e HOME=/tmp \
	-e GOCACHE=/cache/build \
	-e GOMODCACHE=/cache/mod \
	-e GOFLAGS=-buildvcs=false \
	-e GOTOOLCHAIN=local \
	$(1) \
	-w /work/backend $(GO_IMAGE)
DOCKER_GO = $(call go_run,)

$(GO_CACHE):
	@mkdir -p $@

.PHONY: fmt
fmt: | $(GO_CACHE) ## Format the Go sources in place
	$(DOCKER_GO) gofmt -w .

.PHONY: fmt-check
fmt-check: | $(GO_CACHE) ## Fail when a Go file is not gofmt-clean
	@$(DOCKER_GO) sh -c 'out=$$(gofmt -l .); [ -z "$$out" ] || { echo "These files need formatting; run make fmt:"; echo "$$out"; exit 1; }'

.PHONY: vet
vet: | $(GO_CACHE) ## Run go vet over the backend
	$(DOCKER_GO) go vet -tags integration ./...

.PHONY: test-go
test-go: | $(GO_CACHE) ## Run the Go unit tests with the race detector
	$(DOCKER_GO) go test -race $(TESTFLAGS) ./...

.PHONY: build
build: | $(GO_CACHE) ## Build the gotome binary into backend/bin, stamped with VERSION
	$(DOCKER_GO) go build -trimpath -ldflags '$(GO_LDFLAGS)' -o bin/ ./cmd/...

.PHONY: tidy
tidy: | $(GO_CACHE) ## Run go mod tidy
	$(DOCKER_GO) go mod tidy

.PHONY: openapi
openapi: | $(GO_CACHE) ## Regenerate api/openapi.json from the route table
	@mkdir -p $(ROOT)/api
	$(DOCKER_GO) go run ./cmd/gotome openapi ../api/openapi.json

.PHONY: openapi-check
openapi-check: | $(GO_CACHE) ## Fail when api/openapi.json differs from what the code generates
	@$(DOCKER_GO) sh -c 'go run ./cmd/gotome openapi /tmp/openapi.json && diff -u ../api/openapi.json /tmp/openapi.json' \
		|| { echo "api/openapi.json is out of date. Run make openapi and commit the result."; exit 1; }

.PHONY: check-go
check-go: fmt-check vet test-go openapi-check sqlc-check ## The backend gate: formatting, vet, race-checked tests, OpenAPI and sqlc drift
