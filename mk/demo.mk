# The demo for GitHub Pages (#396): the web app against a fake API in the
# browser, with public-domain sample books.

DEMO_BASE ?= /GoTome/

.PHONY: web-demo
web-demo: | $(ROOT)/.cache/npm ## Build the demo into web/dist-demo (base path DEMO_BASE, /GoTome/ for Pages)
	$(DOCKER_NODE) sh -c 'npm ci --no-audit --no-fund && DEMO_BASE=$(DEMO_BASE) npm run build:demo'

# Reaches the internet, so only the owner runs it (and CI for the demo).
# SAMPLE_BOOKS_FROM is a directory (relative to the checkout) whose <id>.epub
# files are used where they match the lock file; only the rest is downloaded.
SAMPLE_BOOKS_FROM ?=
.PHONY: sample-books
sample-books: | $(ROOT)/.cache/npm ## Fetch the demo's public-domain books and covers into web/demo (reaches the internet; SAMPLE_BOOKS_FROM=dir reads books there first)
	$(DOCKER_NODE) sh -c '[ -d node_modules ] || npm ci --no-audit --no-fund; node scripts/sample-books.mjs$(if $(SAMPLE_BOOKS_FROM), --from /src/$(SAMPLE_BOOKS_FROM))'

# The fallback copy of the books for CI, when the sites refuse its runners
# (docs/demo.md). The owner runs it after make sample-books.
DEMO_BOOKS_TAG ?= demo-books
.PHONY: demo-books-release
demo-books-release: ## Pack the verified books of web/demo/files and publish them as the prerelease demo-books (the owner, after make sample-books)
	@ROOT=$(ROOT) TAG=$(DEMO_BOOKS_TAG) sh $(ROOT)/web/scripts/demo-books-release.sh

# The built demo under /GoTome/ as Pages serves it (e2e/demo/serve.mjs), in
# Playwright's image like the rest of the browser tests.
.PHONY: test-demo
test-demo: | $(ROOT)/.cache/npm ## Open the built demo (make web-demo first) in Chromium and click through it
	@[ -f $(ROOT)/web/dist-demo/index.html ] || { echo "No demo build: run make web-demo first."; exit 1; }
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx tsc --noEmit && npx playwright test -c playwright.demo.config.ts'
