# storedog-go-apm-example

A small, self-contained Go application that shows Datadog APM for Go. Clone it, build it, and deploy it into the `storedog` namespace of a local Kubernetes cluster. A built-in load generator starts sending traffic right away, so traces appear in Datadog without any manual clicking.

It is instrumented with [Orchestrion](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/go/), which adds tracing at compile time. There is **no `tracer.Start()` and no `contrib` wrapper** in the application code. The build follows the [Orchestrion Dockerfile guide](https://docs.datadoghq.com/tracing/guide/orchestrion_dockerfile/) and sets `GOFLAGS` so a plain `go build` compiles through Orchestrion.

## What gets deployed

```
go-shop-loadgen ──▶ go-shop-gateway ──▶ go-shop-inventory ──▶ SQLite (database/sql)
                          └───────────▶ go-shop-payment ──▶ card-network (simulated)
```

One image, four roles (`SHOP_ROLE=gateway|inventory|payment|loadgen`), four Deployments, three Services.

| Service | What it shows |
|---|---|
| `go-shop-gateway` | `net/http` server and client, parallel child calls, a custom `checkout.process` span, panic recovery |
| `go-shop-inventory` | `database/sql` queries as spans, an **N+1 query** on `/products`, a slow search path, a background worker with its own root trace |
| `go-shop-payment` | Custom spans, retries with errors, a CPU-heavy `payment.fraud_check` for the profiler |
| `go-shop-loadgen` | Generates a mixed stream of journeys so every trace pattern below shows up on its own |

## Quick start

You do not need Go or Docker. You need `kubectl`, `make` and a cluster with the Datadog Agent running. The cluster pulls the prebuilt image from GitHub Container Registry (`ghcr.io/datadog-asean-se/storedog-go-apm-example`), which a [GitHub Actions workflow](.github/workflows/image.yml) builds for `linux/amd64` and `linux/arm64`.

```bash
git clone https://github.com/datadog-asean-se/storedog-go-apm-example.git
cd storedog-go-apm-example
make deploy            # deploys to namespace storedog, pulling the image from GHCR
make status
```

Then open **APM → Traces** in Datadog and filter on `env:lab service:go-shop-*`. Allow about a minute for the first traces.

To build the image yourself instead (needs Docker, Podman or nerdctl on a local cluster such as kind, minikube, k3d or Docker Desktop):

```bash
make deploy-local
```

Variables you can override: `NAMESPACE` (default `storedog`), `IMAGE`, and `OVERLAY` (see Troubleshooting).

## Error Tracking and Bits Code demo

The code contains planted bugs ("demo bugs") that the load generator triggers a few percent of the time. Each fails in a different way, so [Error Tracking](https://docs.datadoghq.com/error_tracking/) creates a separate issue for each one.

| Endpoint | Error type | Cause | Code |
|---|---|---|---|
| `/api/customer?id=c-999` | `runtime.errorString` (nil pointer dereference) | unknown ID returns `nil`, then `c.Tier` | [bugs.go](internal/gateway/bugs.go) `customer` |
| `/api/recommendations` | `runtime.boundsError` (index out of range) | reads item 4 of a 3-item slice | `recommendations` |
| `/api/discount?qty=0` | `runtime.errorString` (integer divide by zero) | divides by `qty` without a check | `discount` |
| `/api/meta` | `*runtime.TypeAssertionError` | asserts `int` on a JSON number (`float64`) | [inventory.go](internal/inventory/inventory.go) `meta` |
| `/api/panic` | `runtime.plainError` | writes to a nil map | `boom` |
| `POST /api/checkout` | `*json.UnmarshalTypeError` | a string where `qty` expects a number | `decodeItems` |
| `POST /api/checkout` | `*gateway.InsufficientStockError`, `*gateway.UnknownSKUError`, `*gateway.PaymentFailedError`, `*payment.DeclinedError` | typed business errors | `bugs.go`, `payment.go` |

**To show suggested fixes:**
1. Set up the GitHub integration, the [Source Code Integration](https://docs.datadoghq.com/integrations/guide/source-code-integration/) and [Bits Code](https://docs.datadoghq.com/bits_ai/bits_code/setup/) for your fork of this repository. Bits Code needs write access to the repository.
2. The image already carries the commit SHA and repository URL (`DD_GIT_COMMIT_SHA`, `DD_GIT_REPOSITORY_URL`), so stack frames link to the source.
3. Bits Code needs write access to the repository that the stack frames point to. Fork this repository, let the forked workflow build and push the image to your own GHCR (it uses `ghcr.io/<your-account>/storedog-go-apm-example` and your repository URL), make that package public, then deploy it:
   ```bash
   make deploy-versions IMAGE=ghcr.io/<your-account>/storedog-go-apm-example:1.1.0
   ```
4. In **Error Tracking**, open an issue, then **Review**, then **Investigate this issue** ([Bits Investigation](https://docs.datadoghq.com/error_tracking/bits_investigation/), a Preview feature at the time of writing). If the root cause is in code, Bits Code proposes a diff and offers **Create PR**. It never merges on its own.

The Datadog pages I read do not list the languages Bits Code supports, so confirm Go support for your org before relying on this in a live demo.

## Gateway versions to compare in APM

`make deploy-versions` runs three gateway versions at the same time, behind one Kubernetes Service:

| Deployment | `version` | Behaviour |
|---|---|---|
| `shop-gateway` | `1.1.0` | healthy |
| `shop-gateway-v2` | `1.2.0` | about 30% of requests fail with HTTP 500 (`FAULT_MODE=errors`) |
| `shop-gateway-v3` | `1.3.0` | about 30% of requests are slow, with 0.4 to 1.3 s delays and some 2.5 s (`FAULT_MODE=latency`) |

All three are the same image. The faults are switched on by environment variables, which keeps the demo simple, and all three share the service name `go-shop-gateway`. In APM, filter or group by `version` to compare error rate and latency per version.

## Versioning

One file, `VERSION`, holds the app version (semantic versioning, `1.0.0`). It drives:

| Where | What |
|---|---|
| Image tag | the workflow pushes `ghcr.io/datadog-asean-se/storedog-go-apm-example:1.0.0`, plus `latest` and the commit SHA |
| `DD_VERSION` | the pod label `tags.datadoghq.com/version` in `deploy/base` (the Datadog `version` tag on traces, logs and profiles) |
| Binary | `-ldflags "-X main.version=..."`, logged at startup as `build_version` |

To release a new version:

```bash
make set-version V=1.1.0          # updates VERSION, the image tag and the version label in every manifest
git commit -am "Release 1.1.0" && git push
git tag v1.1.0 && git push --tags
make deploy                       # after the workflow has pushed the new image
```

In Datadog, filter traces by `version:1.1.0` and compare it with `version:1.0.0` to see the effect of a change.

## Trace tour: what to look at

1. **Distributed trace.** Open any `POST /api/checkout` trace. One trace spans loadgen → gateway → inventory → SQLite and gateway → payment → card-network.
2. **N+1 queries.** Open a `GET /api/products` trace. The flame graph shows one `SELECT` per product. The loadgen also sends `?optimized=true` (a single `JOIN`); compare the two traces and their durations.
3. **Errors.** Filter `status:error`. You will see declined cards after three retries (`payment`), unknown SKUs (`409`), and a deliberate panic on `/api/panic`, which appears in Error Tracking.
4. **Retries.** In a failed `/charge` trace, count the `acquirer.authorize` spans and read the `retry.attempt` tag.
5. **Parallel work.** In a checkout trace, the per-item `/stock` calls run at the same time as sibling spans.
6. **Profiler.** Open Profiling for `go-shop-payment` and find `fraudCheck`. Large baskets (`amount > 100`) burn more CPU.
7. **Background work.** `inventory.sync` runs every 30 seconds as its own root trace (span type `worker`).
8. **Logs ↔ traces.** The JSON logs carry `dd.trace_id` and `dd.span_id`. Use `make logs`, then jump from a log line to its trace.
9. **Custom tags.** Facet by `cart.items`, `order.value`, `customer.tier` and `product.sku` to slice traces by business attributes.

## How the instrumentation works

**What is Orchestrion?** It is a Datadog tool for Go that adds tracing at compile time. Go has no runtime agent to attach, so Orchestrion instead rewrites the supported libraries while the compiler runs and starts the tracer for you. Your source code keeps no tracing wrappers.

- `orchestrion.tool.go` and `go.mod` pin Orchestrion (created once with `orchestrion pin`).
- The [Dockerfile](Dockerfile) installs the pinned Orchestrion, sets `GOFLAGS="'-toolexec=orchestrion toolexec'"`, and then runs a plain `go build`. At compile time it instruments `net/http`, `database/sql` and `log/slog`, and starts the tracer. Set `DD_PROFILING_ENABLED=true` (the manifests do) to run the continuous profiler.
- The application code only uses the tracer API for **business spans** and tags (`tracer.StartSpanFromContext`, `span.SetTag`). That is optional; the HTTP, SQL and log instrumentation needs no code at all.
- Service, environment and version come from the pod labels `tags.datadoghq.com/*` (Unified Service Tagging).

Run locally without Docker (needs Go 1.26+):

```bash
go install github.com/DataDog/orchestrion
orchestrion go build -o shop ./cmd/shop
# or, equivalently, via GOFLAGS (the whole flag is quoted because it contains a space):
GOFLAGS="'-toolexec=orchestrion toolexec'" go build -o shop ./cmd/shop
```

## Correlating logs with traces

The app writes JSON logs to stdout. Every log line written with a context (`log.InfoContext(ctx, ...)`) carries `dd.trace_id` and `dd.span_id`, which Orchestrion adds to `log/slog` at build time. There is no log-correlation code in this repo. This is what I saw when running it locally; the Datadog [connect logs and traces for Go](https://docs.datadoghq.com/tracing/other_telemetry/connect_logs_and_traces/go/) page documents manual injection and logrus, not Orchestrion.

```bash
make logs     # print the JSON logs and look for dd.trace_id
```

For logs to reach Datadog, **log collection must be enabled on the Agent**: `datadog.logs.enabled` and `datadog.logs.containerCollectAll` in the Helm chart, or `spec.features.logCollection` for the Operator ([Kubernetes log collection](https://docs.datadoghq.com/containers/kubernetes/log/)). The pod annotation `ad.datadoghq.com/shop.logs` in `deploy/base` sets `source:go` and the service name for each container.

In Datadog, open **Logs**, filter `env:lab service:go-shop-*`, open a log line and use **View Trace** (or open a trace and use its **Logs** tab).

## Troubleshooting

**No traces after a few minutes.**

```bash
kubectl -n storedog logs deploy/shop-gateway | head -20     # look for the tracer configuration line and any agent connection errors
kubectl -n storedog get pods -l app=shop-gateway -o yaml | grep -A2 DD_AGENT_HOST
```

If `DD_AGENT_HOST` is missing, the Cluster Agent admission controller did not inject it. Deploy with the overlay that points at the node's Agent (requires APM hostPort 8126 enabled on the Agent):

```bash
make deploy OVERLAY=overlays/hostip
```

**`ImagePullBackOff`.** Check the reason first:

```bash
kubectl -n storedog describe pod -l app=shop-gateway | grep -A5 Events
```

The default image is public on GHCR, so the cluster must be able to reach `ghcr.io`. If it cannot, mirror the image to a registry it can reach and deploy with that name:

```bash
make deploy IMAGE=<registry>/storedog-go-apm-example:latest
```

**Clean up.**

```bash
make destroy
```

## Layout

```
cmd/shop            entrypoint, picks the role from SHOP_ROLE
internal/gateway    public API, fan-out, panic recovery
internal/inventory  SQLite catalogue, N+1, background sync
internal/payment    retries, fraud check
internal/loadgen    synthetic traffic
deploy/base         Kubernetes manifests (kustomize, namespace storedog)
scripts/deploy.sh   deploy (pull from GHCR, or BUILD=1 to build locally)
.github/workflows   builds and pushes the multi-arch image to GHCR
```

This is a demo for learning. It uses simulated payments and in-memory data, and is not production code.
