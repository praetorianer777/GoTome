# The README's pictures (#395), made against this checkout's running stack
# with the public-domain books of web/demo and the audio fixture, never a real
# library. Not part of the gate. The browser writes PNGs to .cache/screenshots
# and ImageMagick turns them into WebP under docs/screenshots.
SHOTS_DIR := $(ROOT)/.cache/screenshots

.PHONY: screenshots
screenshots: | $(NPM_CACHE) ## Make docs/screenshots from a fresh stack (make up first; web/demo/files needs make sample-books)
	@[ -f $(STACK_ENV_FILE) ] && docker compose ps --status running --services 2>/dev/null | grep -qx app \
		|| { echo "The stack for this checkout is not running. Start it with make up, then run this again."; exit 1; }
	@[ -d $(ROOT)/web/demo/files ] || { echo "No books in web/demo/files: run make sample-books first."; exit 1; }
	mkdir -p $(SHOTS_DIR)
	$(DOCKER_PLAYWRIGHT) sh -c '$(E2E_INSTALL) && npx tsc --noEmit && npx playwright test -c playwright.screenshots.config.ts'
	mkdir -p $(ROOT)/docs/screenshots
	for f in $(SHOTS_DIR)/*.png; do \
		magick "$$f" -define webp:method=6 -quality 80 $(ROOT)/docs/screenshots/$$(basename "$$f" .png).webp || exit 1; \
	done
