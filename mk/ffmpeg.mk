# GOtome's own FFmpeg, built by the ffmpeg stage of deploy/Dockerfile from the
# release pinned there. Its source lies in deploy/vendor once make ffmpeg-fetch
# has run, so nothing is fetched; CI, which may download, fetches it itself.

FFMPEG_VERSION := $(shell sed -n 's/^ARG FFMPEG_VERSION=//p' $(ROOT)/deploy/Dockerfile)
FFMPEG_SHA256 := $(shell sed -n 's/^ARG FFMPEG_SHA256=//p' $(ROOT)/deploy/Dockerfile)
FFMPEG_TARBALL := ffmpeg-$(FFMPEG_VERSION).tar.xz
FFMPEG_VENDOR := $(ROOT)/deploy/vendor
# A worktree takes the main checkout's, as a hard link.
FFMPEG_MAIN_VENDOR := $(abspath $(shell git -C $(ROOT) rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/../deploy/vendor)
# The stage's image, tagged by the stage, for the toolchain image to copy from.
FFMPEG_STAGE_SUM := $(shell sed -n -e '/^ARG FFMPEG_/p' -e '/AS ffmpeg$$/,/^$$/p' $(ROOT)/deploy/Dockerfile | cksum | cut -d' ' -f1)
FFMPEG_IMAGE := gotome-ffmpeg:$(FFMPEG_STAGE_SUM)

.PHONY: ffmpeg-fetch
ffmpeg-fetch: ## Fetch FFmpeg's pinned source into deploy/vendor and check it (reaches the internet)
	@mkdir -p $(FFMPEG_VENDOR)
	curl -fsSL --retry 5 --retry-all-errors -o $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL).part https://ffmpeg.org/releases/$(FFMPEG_TARBALL)
	@sha256sum $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL).part
	@echo '$(FFMPEG_SHA256)  $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL).part' | sha256sum -c --quiet - \
		|| { echo "$(FFMPEG_TARBALL) is not the file pinned in deploy/Dockerfile"; rm -f $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL).part; exit 1; }
	@mv $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL).part $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL)
	@echo "deploy/vendor holds $(FFMPEG_TARBALL); FFmpeg now builds offline"

# The source is there, or taken from the main checkout, or, in CI, fetched.
.PHONY: ffmpeg-source
ffmpeg-source:
	@if [ ! -f $(FFMPEG_VENDOR)/$(FFMPEG_TARBALL) ]; then \
		if [ -f $(FFMPEG_MAIN_VENDOR)/$(FFMPEG_TARBALL) ] && [ $(FFMPEG_MAIN_VENDOR) != $(FFMPEG_VENDOR) ]; then \
			ln $(FFMPEG_MAIN_VENDOR)/$(FFMPEG_TARBALL) $(FFMPEG_VENDOR)/; \
		elif [ -n "$$CI" ]; then \
			$(MAKE) --no-print-directory ffmpeg-fetch; \
		else \
			echo "deploy/vendor has no $(FFMPEG_TARBALL): run make ffmpeg-fetch once (it downloads FFmpeg's source)"; exit 1; \
		fi; \
	fi

.PHONY: ffmpeg-image
ffmpeg-image: ffmpeg-source
	@docker image inspect $(FFMPEG_IMAGE) >/dev/null 2>&1 || \
		docker build --quiet --target ffmpeg -f $(ROOT)/deploy/Dockerfile -t $(FFMPEG_IMAGE) $(ROOT)
