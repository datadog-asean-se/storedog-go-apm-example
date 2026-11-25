# Build with Orchestrion, following
# https://docs.datadoghq.com/tracing/guide/orchestrion_dockerfile/
# Orchestrion rewrites the Go source at compile time, so the app has no tracer.Start call.
FROM golang:1.26 AS build
WORKDIR /app

# Dependency layer first, so it is cached until go.mod / go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# go.mod already pins Orchestrion (see orchestrion.tool.go), so this installs the pinned version.
RUN go install github.com/DataDog/orchestrion

# Optional: BuildKit cache for faster rebuilds. The id keeps this cache separate from other projects.
ENV GOCACHE=/go/build-cache
RUN --mount=type=cache,id=storedog-go-apm-example,target=/go/build-cache \
    orchestrion go build -o=main ./cmd/shop

FROM debian:bookworm
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin app
COPY --from=build /app/main /usr/local/bin/main
USER 10001
ENTRYPOINT ["/usr/local/bin/main"]
