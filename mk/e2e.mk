# The browser suite. It runs in Microsoft's Playwright image of exactly the
# version e2e/package.json pins, so the browsers in the image are the ones the
# library expects and nothing is downloaded at run time.
E2E_DIR := $(ROOT)/e2e
PLAYWRIGHT_VERSION := $(shell sed -n 's/.*"@playwright\/test": *"\([0-9][0-9.]*\)".*/\1/p' $(E2E_DIR)/package.json)
PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright:v$(PLAYWRIGHT_VERSION)-noble

# FULL=1 runs every test in every browser, as CI does. Without it the run is
# the push gate's: the tests tagged @smoke, in Chromium.
FULL ?=
E2E_SELECTION = $(if $(FULL),,--project=chromium --grep @smoke)
WORKERS ?= 4
# Narrows a run to tests whose title matches: make test-e2e FULL=1 ONLY=colours
ONLY ?=
E2E_REPORT_PORT ?= 9323

# On the host's network: the browser reaches the app on its published port,
# exactly as a person at this machine does. --ipc=host because Chromium runs
# out of the default 64 MB of shared memory.
DOCKER_PLAYWRIGHT = docker run --rm --init --ipc=host --network host $(DOCKER_NODE_TTY) \
	-u $(UID_GID) \
	-v $(ROOT):/src \
	-v $(NPM_CACHE):/npmcache \
	-e npm_config_cache=/npmcache \
	-e npm_config_update_notifier=false \
	-e HOME=/tmp \
	-e CI \
	-w /src/e2e $(PLAYWRIGHT_IMAGE)

# npm ci only when the lock file changed since the last install, which keeps a
# rerun of one spec to a couple of seconds.
E2E_INSTALL = [ node_modules/.package-lock.json -nt package-lock.json ] || npm ci --no-audit --no-fund

.PHONY: e2e-npm
e2e-npm: | $(NPM_CACHE) ## Run an npm command in the Playwright container: make e2e-npm ARGS="install"
	$(DOCKER_PLAYWRIGHT) npm $(ARGS)

.PHONY: test-e2e
test-e2e: | $(NPM_CACHE) ## Run the browser suite against this checkout's running stack: FULL=1 for everything, ONLY=<grep>
	@[ -f $(STACK_ENV_FILE) ] && docker compose ps --status running --services 2>/dev/null | grep -qx app \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx tsc --noEmit && npx playwright test --workers=$(WORKERS) $(E2E_SELECTION) $(if $(ONLY),--grep "$(ONLY)")'

.PHONY: e2e-report
e2e-report: | $(NPM_CACHE) ## Serve the last browser run's HTML report, traces included
	@echo "Report on http://localhost:$(E2E_REPORT_PORT)"
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx playwright show-report playwright-report --host 127.0.0.1 --port $(E2E_REPORT_PORT)'
