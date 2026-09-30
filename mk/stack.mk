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
STACK_PORT_BASE ?= $(shell echo $$((20000 + $(STACK_HASH) % $(STACK_PORT_SLOTS) * $(STACK_PORT_STRIDE))))
stack_port = $(shell echo $$(($(STACK_PORT_BASE) + $(1))))

GOTOME_PORT := $(call stack_port,0)

# The image is built from this checkout and tagged with its project, so a
# worktree never runs another worktree's build, and nothing is pulled.
GOTOME_IMAGE := $(STACK_PROJECT)-app
GOTOME_ENV ?= development

# The password of the throwaway development stack; the compose file carries
# the same default.
POSTGRES_PASSWORD ?= gotome

export COMPOSE_FILE := $(ROOT)/deploy/docker-compose.yml
export COMPOSE_PROJECT_NAME := $(STACK_PROJECT)
export GOTOME_PORT GOTOME_IMAGE GOTOME_ENV POSTGRES_PASSWORD

# The Go toolchain container on the stack's network, with the database URL the
# integration suite makes its own databases through.
DOCKER_GO_STACK = $(call go_run,--network $(STACK_NET) \
	-e GOTOME_TEST_DATABASE_URL='postgres://gotome:$(POSTGRES_PASSWORD)@db:5432/gotome?sslmode=disable')

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
		> $(STACK_ENV_FILE)

.PHONY: up
up: stack-env ## Build and start the stack in the background, and say where it is
	docker compose up -d --build --wait
	@echo "GOtome  http://localhost:$(GOTOME_PORT)"
	@echo "Port and project name are in $(STACK_ENV_FILE)."

.PHONY: down
down: ## Stop the stack, keeping its data
	docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)

.PHONY: logs
logs: ## Follow the stack's logs: make logs S=app for one service
	docker compose logs -f $(S)

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
test-integration: | $(GO_CACHE) ## Run the integration suite against this checkout's running stack
	@docker compose ps --status running --services 2>/dev/null | grep -qx db \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	$(DOCKER_GO_STACK) go test -race -tags integration -count=1 $(TESTFLAGS) ./test/...

# The pinned database image is what promises pg_search and pgvector; this fails
# the gate when a new pin no longer carries them.
.PHONY: stack-check
stack-check: ## Check the running stack: two services, app answering, extensions available
	@test "$$(docker compose ps --status running --services | sort | tr '\n' ' ')" = "app db " \
		|| { echo "expected exactly the services app and db to run:"; docker compose ps; exit 1; }
	@docker compose exec -T app gotome healthcheck \
		|| { echo "the app does not answer its health check"; exit 1; }
	@for ext in pg_search vector; do \
		docker compose exec -T db psql -U gotome -d gotome -Atc \
			"select 1 from pg_available_extensions where name = '$$ext'" | grep -qx 1 \
			|| { echo "the database image offers no $$ext extension"; exit 1; }; \
	done
	@echo "stack ok: app and db healthy, pg_search and vector available"

.PHONY: stack-down
stack-down: ## Remove the gate's stack and its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)
