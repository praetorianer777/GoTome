# ONNX Runtime's release archives, which the onnxruntime stage of
# deploy/Dockerfile copies from deploy/vendor, so that a build asks GitHub for
# nothing. make onnxruntime-fetch fills it once; CI, which may download,
# fetches them itself.

ORT_VERSION := $(shell sed -n 's/^ARG ORT_VERSION=//p' $(ROOT)/deploy/Dockerfile)
ORT_SHA256_AMD64 := $(shell sed -n 's/^ARG ORT_SHA256_AMD64=//p' $(ROOT)/deploy/Dockerfile)
ORT_SHA256_ARM64 := $(shell sed -n 's/^ARG ORT_SHA256_ARM64=//p' $(ROOT)/deploy/Dockerfile)
ORT_ARCHIVES := onnxruntime-linux-x64-$(ORT_VERSION).tgz onnxruntime-linux-aarch64-$(ORT_VERSION).tgz
ORT_VENDOR := $(ROOT)/deploy/vendor
# A worktree fetches into the main checkout's and links from there.
ORT_MAIN_VENDOR := $(abspath $(shell git -C $(ROOT) rev-parse --path-format=absolute --git-common-dir 2>/dev/null)/../deploy/vendor)

.PHONY: onnxruntime-fetch
onnxruntime-fetch: ## Fetch ONNX Runtime's pinned archives into deploy/vendor and check them (reaches the internet)
	@mkdir -p $(ORT_MAIN_VENDOR)
	@for f in $(ORT_ARCHIVES); do \
		case $$f in *x64*) sum=$(ORT_SHA256_AMD64);; *) sum=$(ORT_SHA256_ARM64);; esac; \
		if [ -f $(ORT_MAIN_VENDOR)/$$f ] && echo "$$sum  $(ORT_MAIN_VENDOR)/$$f" | sha256sum -c --quiet - 2>/dev/null; then \
			echo "$$f is there"; continue; \
		fi; \
		curl -fsSL --connect-timeout 20 --retry 5 --retry-all-errors -o $(ORT_MAIN_VENDOR)/$$f.part \
			https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/$$f || exit 1; \
		sha256sum $(ORT_MAIN_VENDOR)/$$f.part; \
		echo "$$sum  $(ORT_MAIN_VENDOR)/$$f.part" | sha256sum -c --quiet - \
			|| { echo "$$f is not the file pinned in deploy/Dockerfile"; rm -f $(ORT_MAIN_VENDOR)/$$f.part; exit 1; }; \
		mv $(ORT_MAIN_VENDOR)/$$f.part $(ORT_MAIN_VENDOR)/$$f; \
	done
	@$(MAKE) --no-print-directory onnxruntime-source
	@echo "deploy/vendor holds ONNX Runtime $(ORT_VERSION); the image builds without GitHub"

# The archives are there, or taken from the main checkout, or, in CI, fetched.
.PHONY: onnxruntime-source
onnxruntime-source:
	@mkdir -p $(ORT_VENDOR)
	@for f in $(ORT_ARCHIVES); do \
		[ -f $(ORT_VENDOR)/$$f ] && continue; \
		if [ -f $(ORT_MAIN_VENDOR)/$$f ] && [ $(ORT_MAIN_VENDOR) != $(ORT_VENDOR) ]; then \
			ln $(ORT_MAIN_VENDOR)/$$f $(ORT_VENDOR)/; \
		elif [ -n "$$CI" ]; then \
			$(MAKE) --no-print-directory onnxruntime-fetch; break; \
		else \
			echo "deploy/vendor has no $$f: run make onnxruntime-fetch once (it downloads ONNX Runtime's release)"; exit 1; \
		fi; \
	done
