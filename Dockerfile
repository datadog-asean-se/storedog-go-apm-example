# Build with Datadog Orchestrion, following
# https://docs.datadoghq.com/tracing/guide/orchestrion_dockerfile/
#
# What Orchestrion is: a Datadog tool for Go that adds tracing at COMPILE time. Go has no
# runtime agent to attach, so instead of adding tracer.Start() and wrapper code by hand,
# Orchestrion rewrites the supported libraries (net/http, database/sql, log/slog, ...) while
# the compiler runs, and starts the tracer and profiler for you. The source code stays clean.
#
# How it plugs into this build:
#   1. go.mod pins the Orchestrion version (orchestrion.tool.go), so builds are repeatable.
#   2. `go install` puts the orchestrion binary on the PATH.
#   3. GOFLAGS="-toolexec=orchestrion toolexec" makes every compile step run through it.
#      `orchestrion go build` is an equivalent one-line alternative.
#   4. The runtime image holds only the instrumented binary. Nothing else is installed.
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
