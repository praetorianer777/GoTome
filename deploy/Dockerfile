# The one application image. Stages: web (the browser app, built to static
# files), toolchain (what compiles and tests the backend), build, runtime.

FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
# The app's API types are generated from the document one level up.
COPY api/ /src/api/
COPY web/ ./
RUN npm run build

FROM golang:1.27-bookworm AS toolchain
ENV GOFLAGS=-buildvcs=false GOTOOLCHAIN=local

FROM toolchain AS build
WORKDIR /src
ENV CGO_ENABLED=0
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

# Debian rather than scratch or alpine: text extraction and audio probing will
# run poppler and ffprobe from this image, and both come as Debian packages.
FROM debian:bookworm-slim AS runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/* && \
    useradd --uid 10001 --create-home gotome && \
    install -d -o gotome -g gotome /data
COPY --from=build /out/gotome /usr/local/bin/gotome
USER gotome
# What GOtome itself stores: managed libraries, covers. A volume mounted here
# inherits the directory's owner, so the app can write to it.
VOLUME /data
EXPOSE 8080
# The binary probes itself, so the image needs no curl.
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=6 CMD ["gotome", "healthcheck"]
ENTRYPOINT ["gotome"]
CMD ["serve"]
