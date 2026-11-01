# The one application image. Stages: web (the browser app, built to static
# files), onnxruntime (the embedding runtime's library for the target's
# architecture), fulltext (the full-text engine's library, engine/),
# ffmpeg (GOtome's own small FFmpeg), toolchain (what compiles and tests the
# backend), build, runtime.

# Rust for the full-text engine, at the host toolchain's version the crate
# was locked with, pinned by digest; mk/engine.mk reads it from here.
ARG RUST_IMAGE=rust:1.96.0-slim-bookworm@sha256:4732ca96fd086cb9be682050c3f0176288eebaac2b80aa2bcefccfaf198e1950

# FFmpeg's release, checked as ONNX Runtime's is; mk/ffmpeg.mk reads both.
ARG FFMPEG_VERSION=9.0.2
ARG FFMPEG_SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e

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

# The full-text engine (#265), which the binary loads at run time. With the
# crates in engine/vendor (make engine-fetch) it builds offline; without,
# cargo fetches what Cargo.lock pins, as CI does.
FROM ${RUST_IMAGE} AS fulltext
WORKDIR /src/engine
COPY engine/ ./
RUN if [ -d vendor ]; then \
      set -- --offline --config 'source.crates-io.replace-with="vendored"' --config 'source.vendored.directory="vendor"'; \
    fi && \
    cargo build --release --locked "$@" && \
    mkdir /out && cp target/release/libgotome_fulltext.so /out/

# What ingest/audio.go asks of FFmpeg and nothing else: it copies streams and
# decodes none, so no decoder, encoder or filter of its own is built, and a
# hostile file reaches none. The pipe protocol is where ffmpeg writes a cover.
# On the Go image because that has the compiler, and its glibc is the runtime
# stage's. deploy/vendor holds the source after make ffmpeg-fetch, so that
# nothing is fetched; without it the stage fetches it, as CI's release does.
# The toolchain image takes the same programs from this stage (mk/ffmpeg.mk).
FROM golang:1.27-bookworm AS ffmpeg
ARG FFMPEG_VERSION
ARG FFMPEG_SHA256
COPY deploy/vendor/ /vendor/
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils && \
    tarball="/vendor/ffmpeg-$FFMPEG_VERSION.tar.xz" && \
    if [ ! -f "$tarball" ]; then \
      curl -fsSL --connect-timeout 20 --retry 5 --retry-all-errors -o "$tarball" "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz"; \
    fi && \
    echo "$FFMPEG_SHA256  $tarball" | sha256sum -c - && \
    mkdir /src /out && tar -xJf "$tarball" -C /src --strip-components=1 && \
    cd /src && \
    ./configure \
      --disable-everything --disable-autodetect --disable-network --disable-doc \
      --disable-debug --disable-x86asm --disable-avdevice --disable-swscale --disable-swresample \
      --enable-protocol=file,pipe \
      --enable-demuxer=mov,mp3,flac,ogg \
      --enable-parser=aac,flac,mpegaudio,opus,vorbis \
      --enable-muxer=image2pipe && \
    make -j"$(nproc)" ffmpeg ffprobe && \
    strip -o /out/ffmpeg ffmpeg && strip -o /out/ffprobe ffprobe

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

# Debian rather than scratch or alpine: text extraction runs poppler from this
# image, which comes as a Debian package, and the Go binary and FFmpeg need
# this glibc. The test toolchain (toolchain.Dockerfile) has the same programs.
FROM debian:bookworm-slim AS runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata poppler-utils && \
    rm -rf /var/lib/apt/lists/* && \
    useradd --uid 10001 --create-home gotome && \
    install -d -o gotome -g gotome /data
COPY --from=build /out/gotome /usr/local/bin/gotome
COPY --from=onnxruntime /out/libonnxruntime.so.1.30.0 /usr/local/lib/
COPY --from=fulltext /out/libgotome_fulltext.so /usr/local/lib/
COPY --from=ffmpeg /out/ffmpeg /out/ffprobe /usr/local/bin/
USER gotome
# What GOtome itself stores: managed libraries, covers. A volume mounted here
# inherits the directory's owner, so the app can write to it.
VOLUME /data
EXPOSE 8080
# The binary probes itself, so the image needs no curl.
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=6 CMD ["gotome", "healthcheck"]
ENTRYPOINT ["gotome"]
CMD ["serve"]
