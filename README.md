# storedog-go-apm-example

A small, self-contained Go application that shows Datadog APM for Go. Clone it, build it, and deploy it into the `storedog` namespace of a local Kubernetes cluster. A built-in load generator starts sending traffic right away, so traces appear in Datadog without any manual clicking.

It is instrumented with [Orchestrion](https://docs.datadoghq.com/tracing/trace_collection/automatic_instrumentation/dd_libraries/go/), which adds tracing at compile time. There is **no `tracer.Start()` and no `contrib` wrapper** in the application code. The build follows the [Orchestrion Dockerfile guide](https://docs.datadoghq.com/tracing/guide/orchestrion_dockerfile/).

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

You do not need Go installed. You need Docker (or Podman), `kubectl`, and a local cluster (kind, minikube, k3d or Docker Desktop) with the Datadog Agent running.

```bash
git clone https://github.com/datadog-asean-se/storedog-go-apm-example.git
cd storedog-go-apm-example
make deploy            # builds the image, loads it into the cluster, deploys to namespace storedog
make status
```

Then open **APM → Traces** in Datadog and filter on `env:lab service:go-shop-*`. Allow about a minute for the first traces.

Variables you can override: `NAMESPACE` (default `storedog`), `IMAGE`, and `OVERLAY` (see Troubleshooting).

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

- `orchestrion.tool.go` and `go.mod` pin Orchestrion (created once with `orchestrion pin`).
- The [Dockerfile](Dockerfile) installs the pinned Orchestrion and builds with `orchestrion go build`. At compile time it instruments `net/http`, `database/sql` and `log/slog`, and starts the tracer. Set `DD_PROFILING_ENABLED=true` (the manifests do) to run the continuous profiler.
- The application code only uses the tracer API for **business spans** and tags (`tracer.StartSpanFromContext`, `span.SetTag`). That is optional; the HTTP, SQL and log instrumentation needs no code at all.
- Service, environment and version come from the pod labels `tags.datadoghq.com/*` (Unified Service Tagging).

Run locally without Docker (needs Go 1.26+):

```bash
go install github.com/DataDog/orchestrion
orchestrion go build -o shop ./cmd/shop
```

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

**`ImagePullBackOff`.** The cluster cannot see your local image. Push it to a registry the cluster can pull from and run `make deploy IMAGE=<registry>/<name>:<tag>`.

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
scripts/deploy.sh   build, load, deploy
```

This is a demo for learning. It uses simulated payments and in-memory data, and is not production code.
