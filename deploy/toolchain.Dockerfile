# The image the Go tests and tools run in (mk/go.mk): the Go toolchain and the
# programs the app runs as subprocesses, in the versions the app image has,
# and LuaJIT, which KOReader runs its plugins in, for the plugin's tests.
# Built without a context: FFmpeg comes from the image of deploy/Dockerfile's
# ffmpeg stage (mk/ffmpeg.mk), and the rest from this file.
ARG GO_BASE=golang:1.27-bookworm
ARG FFMPEG_IMAGE
FROM ${FFMPEG_IMAGE} AS ffmpeg
FROM ${GO_BASE}
RUN apt-get update && \
    apt-get install -y --no-install-recommends poppler-utils libmobi-tools luajit && \
    rm -rf /var/lib/apt/lists/*
COPY --from=ffmpeg /out/ffmpeg /out/ffprobe /usr/local/bin/
