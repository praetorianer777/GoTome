# The full-text engine (engine/, #265): a Rust library the binary loads at
# run time. It builds in the Rust image the Dockerfile pins, from the crates
# vendored into engine/vendor, so that nothing is fetched once
# make engine-fetch has run; CI, which may download, fetches them itself.

RUST_IMAGE ?= $(shell sed -n 's/^ARG RUST_IMAGE=//p' $(ROOT)/deploy/Dockerfile)
ENGINE_VENDOR := $(ROOT)/engine/vendor
ENGINE_OUT := $(ROOT)/.cache/engine
# What the Go tests load (GOTOME_TEST_FULLTEXT, mk/go.mk).
ENGINE_LIB := $(ENGINE_OUT)/libgotome_fulltext.so
# A worktree takes the main checkout's crates, as hard links.
ENGINE_MAIN_VENDOR := $(abspath $(shell git -C $(ROOT) rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/../engine/vendor)

engine_run = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-e HOME=/tmp \
	-e CARGO_HOME=/work/.cache/engine/cargo \
	-e CARGO_TARGET_DIR=/work/.cache/engine/target \
	$(1) \
	-w /work/engine $(RUST_IMAGE)
ENGINE_OFFLINE := --offline --config 'source.crates-io.replace-with="vendored"' --config 'source.vendored.directory="vendor"'

.PHONY: engine-fetch
engine-fetch: ## Fetch what the engine builds from: the pinned Rust image and its crates into engine/vendor (reaches the internet)
	@mkdir -p $(ENGINE_OUT)
	docker pull $(RUST_IMAGE)
	$(engine_run) cargo vendor --locked --versioned-dirs vendor >/dev/null
	@echo "engine/vendor holds the engine's crates; the engine now builds offline"

# The crates are there, or taken from the main checkout, or, in CI, fetched.
.PHONY: engine-vendor
engine-vendor:
	@if [ ! -d $(ENGINE_VENDOR) ]; then \
		if [ -d $(ENGINE_MAIN_VENDOR) ] && [ $(ENGINE_MAIN_VENDOR) != $(ENGINE_VENDOR) ]; then \
			cp -al $(ENGINE_MAIN_VENDOR) $(ENGINE_VENDOR); \
		elif [ -n "$$CI" ]; then \
			$(MAKE) --no-print-directory engine-fetch; \
		else \
			echo "engine/vendor is missing: run make engine-fetch once (it downloads the Rust image and the crates)"; exit 1; \
		fi; \
	fi

.PHONY: check-engine
check-engine: engine-vendor ## Test the full-text engine and build its library for the Go tests
	@mkdir -p $(ENGINE_OUT)
	$(engine_run) cargo test --locked $(ENGINE_OFFLINE)
	$(engine_run) cargo build --release --locked $(ENGINE_OFFLINE)
	cp $(ENGINE_OUT)/target/release/libgotome_fulltext.so $(ENGINE_LIB)
