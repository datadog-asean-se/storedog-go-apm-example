#!/usr/bin/env bash
# Deploy the example to the storedog namespace.
#   ./scripts/deploy.sh            pull the prebuilt image from GitHub Container Registry (no Docker needed)
#   BUILD=1 ./scripts/deploy.sh    build the image locally with Docker and load it into the cluster
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="$(tr -d '[:space:]' < VERSION)"
REGISTRY_IMAGE="ghcr.io/datadog-asean-se/storedog-go-apm-example:${VERSION}"
IMAGE="${IMAGE:-$REGISTRY_IMAGE}"
NAMESPACE="${NAMESPACE:-storedog}"
OVERLAY="${OVERLAY:-base}"   # base | overlays/hostip
BUILD="${BUILD:-0}"

CTX="$(kubectl config current-context)"
echo "==> kubectl context: ${CTX}  namespace: ${NAMESPACE}  image: ${IMAGE}  DD_VERSION: ${VERSION}"

PULL_POLICY="Always"
if [ "${BUILD}" = "1" ]; then
  [ "${IMAGE}" != "${REGISTRY_IMAGE}" ] || IMAGE="storedog-go-shop:local"
  BUILDER=""
  for b in docker podman nerdctl; do command -v "$b" >/dev/null 2>&1 && BUILDER="$b" && break; done
  [ -n "$BUILDER" ] || { echo "BUILD=1 needs docker, podman or nerdctl" >&2; exit 1; }
  echo "==> Building ${IMAGE} with ${BUILDER}"
  DOCKER_BUILDKIT=1 "$BUILDER" build --build-arg VERSION="${VERSION}" --build-arg DD_GIT_COMMIT_SHA="$(git rev-parse HEAD)" -t "${IMAGE}" .
  case "${CTX}" in
    kind-*)     kind load docker-image "${IMAGE}" --name "${CTX#kind-}" ;;
    minikube*)  minikube image load "${IMAGE}" ;;
    k3d-*)      k3d image import "${IMAGE}" -c "${CTX#k3d-}" ;;
    docker-desktop|rancher-desktop|colima*) echo "==> Local image store is shared, nothing to load" ;;
    *) echo "==> Unknown cluster type: push ${IMAGE} to a registry the cluster can pull from, then run with IMAGE=<registry>/<name>:<tag>" ;;
  esac
  PULL_POLICY="IfNotPresent"
fi

kubectl get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl create ns "${NAMESPACE}"
kubectl kustomize "deploy/${OVERLAY}" \
  | sed -e "s|image: ${REGISTRY_IMAGE}|image: ${IMAGE}|" -e "s|imagePullPolicy: Always|imagePullPolicy: ${PULL_POLICY}|" \
  | kubectl apply -n "${NAMESPACE}" -f -
# Restart so the pods pick up the image and version label.
DEPLOYS="shop-inventory shop-payment shop-gateway shop-loadgen"
kubectl -n "${NAMESPACE}" get deploy shop-gateway-v2 >/dev/null 2>&1 && DEPLOYS="${DEPLOYS} shop-gateway-v2 shop-gateway-v3"
[ "${OVERLAY}" = "overlays/versions" ] && DEPLOYS="shop-inventory shop-payment shop-gateway shop-gateway-v2 shop-gateway-v3 shop-loadgen"
for d in ${DEPLOYS}; do kubectl -n "${NAMESPACE}" rollout restart "deploy/${d}" >/dev/null; done
for d in ${DEPLOYS}; do
  kubectl -n "${NAMESPACE}" rollout status "deploy/${d}" --timeout=180s
done

cat <<MSG

Deployed. The load generator is already sending requests.
Open Datadog APM and filter:  env:lab  service:go-shop-*
Traces appear within about a minute.
MSG
