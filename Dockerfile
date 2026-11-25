# Build with Orchestrion, following
# https://docs.datadoghq.com/tracing/guide/orchestrion_dockerfile/
# Orchestrion rewrites the Go source at compile time, so the app has no tracer.Start call.
# This file uses the GOFLAGS -toolexec method; `orchestrion go build` is the equivalent one-liner.
# Build on the runner's own architecture and cross-compile, so arm64 images need no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app

# Dependency layer first, so it is cached until go.mod / go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# go.mod already pins Orchestrion (see orchestrion.tool.go), so this installs the pinned version.
RUN go install github.com/DataDog/orchestrion

# From here on every `go` command compiles through Orchestrion (the GOFLAGS toolexec method).
# Set it AFTER `go install orchestrion`, because the toolexec needs the orchestrion binary to exist.
# The whole flag is quoted because it contains a space.
ENV GOFLAGS="'-toolexec=orchestrion toolexec'"

# Optional: BuildKit cache for faster rebuilds. The id keeps this cache separate from other projects.
ENV GOCACHE=/go/build-cache
RUN --mount=type=cache,id=storedog-go-apm-example,target=/go/build-cache \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o=main ./cmd/shop

FROM debian:bookworm
LABEL org.opencontainers.image.source="https://github.com/datadog-asean-se/storedog-go-apm-example"
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin app
COPY --from=build /app/main /usr/local/bin/main
USER 10001
ENTRYPOINT ["/usr/local/bin/main"]
