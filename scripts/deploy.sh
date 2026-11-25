#!/usr/bin/env bash
# Build the image with Orchestrion (see Dockerfile), make it available to the
# local cluster, and deploy to the storedog namespace.
set -euo pipefail

IMAGE="${IMAGE:-storedog-go-shop:latest}"
NAMESPACE="${NAMESPACE:-storedog}"
OVERLAY="${OVERLAY:-base}"   # base | overlays/hostip
cd "$(dirname "$0")/.."

CTX="$(kubectl config current-context)"
echo "==> kubectl context: ${CTX}  namespace: ${NAMESPACE}"

# 1. Build
BUILDER=""
for b in docker podman nerdctl; do command -v "$b" >/dev/null 2>&1 && BUILDER="$b" && break; done
[ -n "$BUILDER" ] || { echo "No docker, podman or nerdctl found" >&2; exit 1; }
echo "==> Building ${IMAGE} with ${BUILDER}"
DOCKER_BUILDKIT=1 "$BUILDER" build -t "${IMAGE}" .

# 2. Load into the local cluster (skipped for Docker Desktop, which shares the image store)
case "${CTX}" in
  kind-*)     kind load docker-image "${IMAGE}" --name "${CTX#kind-}" ;;
  minikube*)  minikube image load "${IMAGE}" ;;
  k3d-*)      k3d image import "${IMAGE}" -c "${CTX#k3d-}" ;;
  docker-desktop|rancher-desktop|colima*) echo "==> Local image store is shared, nothing to load" ;;
  *) echo "==> Unknown cluster type. If pods show ImagePullBackOff, push ${IMAGE} to a registry the cluster can pull from and set IMAGE=<registry>/<name>:<tag>." ;;
esac

# 3. Deploy
kubectl get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl create ns "${NAMESPACE}"
if [ "${IMAGE}" != "storedog-go-shop:latest" ]; then
  (cd "deploy/${OVERLAY}" && kubectl kustomize . | sed "s|image: storedog-go-shop:latest|image: ${IMAGE}|") | kubectl apply -n "${NAMESPACE}" -f -
else
  kubectl apply -n "${NAMESPACE}" -k "deploy/${OVERLAY}"
fi
# Recreate pods so a rebuilt :latest image is picked up.
kubectl -n "${NAMESPACE}" rollout restart deploy/shop-gateway deploy/shop-inventory deploy/shop-payment deploy/shop-loadgen
for d in shop-inventory shop-payment shop-gateway shop-loadgen; do
  kubectl -n "${NAMESPACE}" rollout status "deploy/${d}" --timeout=180s
done

cat <<MSG

Deployed. The load generator is already sending requests.
Open Datadog APM and filter:  env:lab  service:go-shop-*
Traces appear within about a minute.
MSG
