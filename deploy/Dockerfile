# The one application image. Stages: web (the browser app, built to static
# files), onnxruntime (the embedding runtime's library for the target's
# architecture), toolchain (what compiles and tests the backend), build,
# runtime.

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
# The app's API types are generated from the document one level up.
COPY api/ /src/api/
COPY web/ ./
RUN npm run build

# ONNX Runtime's own release for both architectures, each checked against
# the SHA-256 GitHub publishes for it (docs/decisions/embedding-runtime.md);
# the stage runs on the target's architecture and keeps that one's library.
# Both are fetched because the classic builder, which compose may use, has
# neither TARGETARCH in FROM nor skips stages nothing copies from.
FROM debian:bookworm-slim AS onnxruntime
ADD https://github.com/microsoft/onnxruntime/releases/download/v1.30.0/onnxruntime-linux-x64-1.30.0.tgz /ort-amd64.tgz
ADD https://github.com/microsoft/onnxruntime/releases/download/v1.30.0/onnxruntime-linux-aarch64-1.30.0.tgz /ort-arm64.tgz
RUN printf '%s\n' \
      'a5ed5a3cac51fbb2e90da632ae43d19212faaa20e76484e62bcb7c23ddb3b3fd  /ort-amd64.tgz' \
      'e16a27a8ed330bbc698df7330b0cf56e722f354e3bcc92118682c74ef3c3e3da  /ort-arm64.tgz' \
      | sha256sum -c - && \
    mkdir /out && \
    tar -xzf "/ort-$(dpkg --print-architecture).tgz" -C /out --strip-components=2 \
      --wildcards '*/lib/libonnxruntime.so.1.30.0'

FROM golang:1.27-bookworm AS toolchain
ENV GOFLAGS=-buildvcs=false GOTOOLCHAIN=local

FROM toolchain AS build
WORKDIR /src
# cgo for ONNX Runtime, which the binary loads at run time: nothing of it is
# linked in, but the binary now needs the glibc of the runtime stage.
ENV CGO_ENABLED=1
# The glob keeps this working before the module has its first dependency and
# with it a go.sum.
COPY backend/go.mod backend/go.sum* ./
RUN go mod download

COPY backend/ ./
# The binary embeds the web app from this directory.
COPY --from=web /src/web/dist/ ./internal/webui/dist/
COPY VERSION /VERSION
ARG COMMIT=""
RUN PKG=github.com/praetorianer777/gotome/backend/internal/version && \
    go build -trimpath \
      -ldflags="-s -w -X $PKG.Version=$(cat /VERSION) -X $PKG.Commit=$COMMIT" \
      -o /out/gotome ./cmd/gotome

# Debian rather than scratch or alpine: text extraction and audio probing run
# poppler and ffprobe from this image, and both come as Debian packages. The
# test toolchain (toolchain.Dockerfile) installs the same ones.
FROM debian:bookworm-slim AS runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata poppler-utils ffmpeg && \
    rm -rf /var/lib/apt/lists/* && \
    useradd --uid 10001 --create-home gotome && \
    install -d -o gotome -g gotome /data
COPY --from=build /out/gotome /usr/local/bin/gotome
COPY --from=onnxruntime /out/libonnxruntime.so.1.30.0 /usr/local/lib/
USER gotome
# What GOtome itself stores: managed libraries, covers. A volume mounted here
# inherits the directory's owner, so the app can write to it.
VOLUME /data
EXPOSE 8080
# The binary probes itself, so the image needs no curl.
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=6 CMD ["gotome", "healthcheck"]
ENTRYPOINT ["gotome"]
CMD ["serve"]
