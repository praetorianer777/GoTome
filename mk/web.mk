# The web app. Node runs in a container like every other toolchain; the npm
# cache is bind-mounted under .cache so it is owned by the invoking user.
NODE_IMAGE ?= node:24-alpine
NPM_CACHE  := $(ROOT)/.cache/npm

# A terminal only when there is one, so the gate runs the same in CI.
DOCKER_NODE_TTY := $(shell [ -t 0 ] && echo -t)

# The whole checkout is mounted, not just web/, so the app can read
# api/openapi.json and VERSION.
DOCKER_NODE = docker run --rm $(DOCKER_NODE_TTY) \
	-u $(UID_GID) \
	-v $(ROOT):/src \
	-v $(NPM_CACHE):/npmcache \
	-e npm_config_cache=/npmcache \
	-e npm_config_update_notifier=false \
	-e HOME=/tmp \
	-w /src/web $(NODE_IMAGE)

# One dev server port per checkout, so worktrees running side by side do not
# fight over 5173.
WEB_DEV_PORT ?= $(shell echo $$((5200 + $(STACK_HASH) % 700)))

# Where the Go binary embeds the built app from.
WEB_EMBED_DIR := $(ROOT)/backend/internal/webui/dist

$(NPM_CACHE):
	@mkdir -p $@

.PHONY: npm
npm: | $(NPM_CACHE) ## Run an npm command in the web container: make npm ARGS="install"
	$(DOCKER_NODE) npm $(ARGS)

.PHONY: web-schema
web-schema: | $(NPM_CACHE) ## Regenerate web/src/api/schema.d.ts from api/openapi.json
	$(DOCKER_NODE) sh -c '[ -d node_modules ] || npm ci --no-audit --no-fund; npm run schema'

.PHONY: check-web
check-web: | $(NPM_CACHE) ## The web gate: lint, types, unit tests, API types current, production build
	$(DOCKER_NODE) sh -c 'npm ci --no-audit --no-fund && npm run lint && npm run typecheck && npm test && npm run schema:check && npm run build'

.PHONY: test-web
test-web: | $(NPM_CACHE) ## Run the web app's unit tests
	$(DOCKER_NODE) sh -c '[ -d node_modules ] || npm ci --no-audit --no-fund; npm test'

.PHONY: web-build
web-build: | $(NPM_CACHE) ## Production build of the web app into web/dist
	$(DOCKER_NODE) sh -c 'npm ci --no-audit --no-fund && npm run build'

# The Go binary embeds this directory. It is empty in a checkout, where the
# binary then says the app was not built; the image build fills it the same way.
.PHONY: web-embed
web-embed: web-build ## Build the web app and place it where the Go binary embeds it
	@find $(WEB_EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
	@cp -r $(ROOT)/web/dist/. $(WEB_EMBED_DIR)/

.PHONY: web-dev
web-dev: | $(NPM_CACHE) ## Vite dev server for this checkout, proxying /api to its running stack
	@echo "GOtome web on http://localhost:$(WEB_DEV_PORT), API from http://localhost:$(GOTOME_PORT)"
	docker run --rm --init $(if $(DOCKER_NODE_TTY),-it) \
		-u $(UID_GID) \
		-v $(ROOT):/src \
		-v $(NPM_CACHE):/npmcache \
		-e npm_config_cache=/npmcache \
		-e HOME=/tmp \
		-e VITE_API_PROXY=http://host.docker.internal:$(GOTOME_PORT) \
		--add-host host.docker.internal:host-gateway \
		-p $(WEB_DEV_PORT):5173 \
		-w /src/web $(NODE_IMAGE) sh -c '[ -d node_modules ] || npm ci --no-audit --no-fund; npm run dev'
