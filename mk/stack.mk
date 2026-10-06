# The compose stack. The project name and the published port derive from the
# checkout's path, so worktrees running their gates side by side never share a
# database or collide on a port. Every compose call in a recipe finds the file
# and the project through the exported variables below.

STACK_HASH := $(shell printf '%s' '$(ROOT)' | cksum | cut -d' ' -f1)
STACK_PROJECT := gotome-$(STACK_HASH)
STACK_NET := $(STACK_PROJECT)_default

# Twenty ports per checkout between 20000 and 29999, below the kernel's
# ephemeral range so an outgoing connection never holds one of them.
STACK_PORT_SLOTS := 500
STACK_PORT_STRIDE := 20
# A slot whose first port another program holds (another project with the
# same scheme, say) is passed over for the next one; the one this checkout's
# own stack holds is kept.
STACK_PORT_BASE ?= $(shell \
	slot=$$(( $(STACK_HASH) % $(STACK_PORT_SLOTS) )); \
	for _ in $$(seq $(STACK_PORT_SLOTS)); do \
		port=$$(( 20000 + slot * $(STACK_PORT_STRIDE) )); \
		if ! ss -Hltn "sport = :$$port" 2>/dev/null | grep -q . || \
			[ "$$(docker ps --filter publish=$$port --format '{{.Label "com.docker.compose.project"}}' 2>/dev/null)" = "$(STACK_PROJECT)" ]; then \
			echo $$port; break; \
		fi; \
		slot=$$(( (slot + 1) % $(STACK_PORT_SLOTS) )); \
	done)
stack_port = $(shell echo $$(($(STACK_PORT_BASE) + $(1))))

GOTOME_PORT := $(call stack_port,0)
# Keycloak, for the single sign-on tests (make sso-up), on the next port,
# inside its container too: the app and the browser use one issuer URL.
KEYCLOAK_PORT := $(call stack_port,1)
KEYCLOAK_URL := http://keycloak:$(KEYCLOAK_PORT)

# The image is built from this checkout and tagged with its project, so a
# worktree never runs another worktree's build, and nothing is pulled.
GOTOME_IMAGE := $(STACK_PROJECT)-app
GOTOME_ENV ?= development

# The deployment's compose file, and on top of it what only development and
# the tests need.
export COMPOSE_FILE := $(ROOT)/deploy/docker-compose.yml:$(ROOT)/deploy/docker-compose.dev.yml
export COMPOSE_PROJECT_NAME := $(STACK_PROJECT)
export GOTOME_PORT GOTOME_IMAGE GOTOME_ENV KEYCLOAK_PORT

# The Go toolchain container on the stack's network, with the database URL the
# integration suite makes its own databases through. The password is the one
# the stack's init wrote, read when the recipe runs.
DOCKER_GO_STACK = $(call go_run,--network $(STACK_NET) \
	-e GOTOME_TEST_DATABASE_URL="postgres://gotome:$$(docker compose exec -T db cat /secrets/db-password)@db:5432/gotome?sslmode=disable")

# What the stack publishes, for the browser suite and for people: a shell can
# source it, and so can a Playwright config.
STACK_ENV_FILE := $(ROOT)/.cache/stack.env

.PHONY: stack-env
stack-env:
	@mkdir -p $(dir $(STACK_ENV_FILE))
	@printf '%s\n' \
		'COMPOSE_PROJECT_NAME=$(STACK_PROJECT)' \
		'STACK_NETWORK=$(STACK_NET)' \
		'GOTOME_PORT=$(GOTOME_PORT)' \
		'GOTOME_URL=http://localhost:$(GOTOME_PORT)' \
		'KEYCLOAK_URL=$(KEYCLOAK_URL)' \
		> $(STACK_ENV_FILE)

.PHONY: up
up: stack-env ## Build and start the stack in the background, and say where it is
	docker compose up -d --build --wait
	@echo "GOtome  http://localhost:$(GOTOME_PORT)"
	@echo "Port and project name are in $(STACK_ENV_FILE)."

.PHONY: sso-up
sso-up: stack-env ## Start Keycloak beside the stack, for the single sign-on browser tests
	COMPOSE_PROFILES=sso docker compose up -d --wait --quiet-pull keycloak
	@echo "Keycloak  $(KEYCLOAK_URL), realm gotome; from this machine http://localhost:$(KEYCLOAK_PORT)"

.PHONY: down
down: ## Stop the stack, keeping its data
	COMPOSE_PROFILES=sso docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	COMPOSE_PROFILES=sso docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)

.PHONY: logs
logs: ## Follow the stack's logs: make logs S=app for one service
	docker compose logs -f $(S)

.PHONY: stack-logs
stack-logs: ## Print the stack's logs so far and return
	docker compose logs --no-color

.PHONY: psql
psql: ## Open psql in the running stack's database
	docker compose exec db psql -U gotome -d gotome

.PHONY: stack-up
stack-up: stack-env ## The gate's stack: build and start it, and wait until both services are healthy
	docker compose up -d --build --wait --quiet-pull

# The suite runs against the stack's Postgres rather than one of its own, so
# the gate never starts a second database. Each test gets a database copied
# from a migrated template (internal/db/dbtest), so tests do not see each other.
.PHONY: test-integration
test-integration: | $(GO_CACHE) go-toolchain ## Run the integration suite against this checkout's running stack
	@docker compose ps --status running --services 2>/dev/null | grep -qx db \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	$(DOCKER_GO_STACK) go test -race -tags integration -count=1 $(TESTFLAGS) ./test/...

# The main views on a library of 50,000 books (test/scale_test.go), with the
# plans of their queries written to .cache/scale-plans, and with the
# app, which runs inside the test, held to the 2 GB a modest server gives it.
# Without the race detector, which would measure itself. Minutes, so not in
# the gate.
.PHONY: scale-test
scale-test: | $(GO_CACHE) go-toolchain ## Time the main views at 50,000 books and check their plans, against the running stack
	@docker compose ps --status running --services 2>/dev/null | grep -qx db \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	$(call go_run,--memory 2g --network $(STACK_NET) -e GOTOME_SCALE=1 -e GOTOME_SCALE_PLANS=/work/.cache/scale-plans \
		-e GOTOME_TEST_DATABASE_URL="postgres://gotome:$$(docker compose exec -T db cat /secrets/db-password)@db:5432/gotome?sslmode=disable") \
		go test -tags integration -count=1 -timeout 30m -v -run TestTheMainViewsAtFiftyThousandBooks ./test/

# The pinned database image is what promises pg_search and pgvector; this fails
# the gate when a new pin no longer carries them.
.PHONY: stack-check
stack-check: ## Check the running stack: two services, web app and API answering, extensions and programs available
	@test "$$(docker compose ps --status running --services | grep -vx keycloak | sort | tr '\n' ' ')" = "app db " \
		|| { echo "expected exactly the services app and db to run, beside the tests' Keycloak:"; docker compose ps; exit 1; }
	@docker compose exec -T app gotome healthcheck \
		|| { echo "the app does not answer its health check"; exit 1; }
	@test "$$(docker compose exec -T app gotome version | cut -d' ' -f1)" = "$(VERSION)" \
		|| { echo "the image does not report the version in VERSION ($(VERSION))"; exit 1; }
	@curl -fsS http://localhost:$(GOTOME_PORT)/ | grep -q 'id="root"' \
		|| { echo "the app does not serve the web app at /"; exit 1; }
	@curl -fsS http://localhost:$(GOTOME_PORT)/api/v1/version | grep -q '"version"' \
		|| { echo "the app does not serve the API under /api/v1"; exit 1; }
	@for ext in pg_search vector; do \
		docker compose exec -T db psql -U gotome -d gotome -Atc \
			"select 1 from pg_available_extensions where name = '$$ext'" | grep -qx 1 \
			|| { echo "the database image offers no $$ext extension"; exit 1; }; \
	done
	@for program in pdfinfo pdftotext pdftoppm ffprobe ffmpeg prlimit; do \
		docker compose exec -T app sh -c "command -v $$program" >/dev/null \
			|| { echo "the app image has no $$program"; exit 1; }; \
	done
	@docker compose exec -T app gotome embed-check >/dev/null \
		|| { echo "the app image cannot load ONNX Runtime"; exit 1; }
	@echo "stack ok: app and db healthy, web app and API served, pg_search and vector available, poppler, ffmpeg and ONNX Runtime installed"

# The embedding model is downloaded into EMBED_MODEL_CACHE when it is not
# there already, so this reaches Hugging Face once per cache; the gate leaves
# it to the full run. The image checks the model against the pinned hashes,
# so a cache filled by spikes/embed/fetch.sh serves as well.
EMBED_MODEL_CACHE ?= $(ROOT)/.cache/embed/model

.PHONY: embed-check
embed-check: ## Run the app image's embedding model against the reference vectors (downloads the model once)
	@mkdir -p $(EMBED_MODEL_CACHE)
	docker run --rm -u $(UID_GID) \
		-e GOTOME_MODEL_DIR=/models -e HOME=/tmp \
		-v $(EMBED_MODEL_CACHE):/models/multilingual-e5-small \
		-v $(ROOT)/backend/internal/embed/testdata:/reference:ro \
		$(GOTOME_IMAGE) embed-check -reference /reference/reference.json


.PHONY: upgrade-test
upgrade-test: ## Upgrade an earlier image's installation to this checkout's, and restore its backup into a new one (builds both images)
	docker compose build app
	@# A stack of its own, beside the checkout's.
	tests/test-upgrade.sh "$$(tests/upgrade-from.sh)" $(GOTOME_IMAGE) $(STACK_PROJECT)-upgrade

.PHONY: stack-down
stack-down: ## Remove the gate's stack and its volumes
	COMPOSE_PROFILES=sso docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)
