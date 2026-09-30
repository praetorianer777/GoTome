# The image the Go tests and tools run in (mk/go.mk): the Go toolchain and the
# programs the app runs as subprocesses, in the versions the app image has.
# Built without a context, so the only input is this file.
ARG GO_BASE=golang:1.27-bookworm
FROM ${GO_BASE}
RUN apt-get update && \
    apt-get install -y --no-install-recommends poppler-utils libmobi-tools && \
    rm -rf /var/lib/apt/lists/*
