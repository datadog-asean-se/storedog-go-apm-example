#!/usr/bin/env bash
# Deploy the example to the storedog namespace.
#   ./scripts/deploy.sh            pull the prebuilt image from GitHub Container Registry (no Docker needed)
#   BUILD=1 ./scripts/deploy.sh    build the image locally with Docker and load it into the cluster
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="$(tr -d '[:space:]' < VERSION)"
REGISTRY_REPO="ghcr.io/datadog-asean-se/storedog-go-apm-example"
IMAGE="${IMAGE:-${REGISTRY_REPO}:${VERSION}}"
IMAGE_REPO="${IMAGE%:*}"      # the tag comes from each manifest, so every version keeps its own tag
NAMESPACE="${NAMESPACE:-storedog}"
OVERLAY="${OVERLAY:-overlays/versions}"   # overlays/versions (default, three gateway versions) | base (one gateway) | overlays/hostip
BUILD="${BUILD:-0}"

CTX="$(kubectl config current-context)"
echo "==> kubectl context: ${CTX}  namespace: ${NAMESPACE}  image: ${IMAGE_REPO}  stable DD_VERSION: ${VERSION}"

PULL_POLICY="Always"
if [ "${BUILD}" = "1" ]; then
  IMAGE_REPO="storedog-go-shop"
  BUILDER=""
  for b in docker podman nerdctl; do command -v "$b" >/dev/null 2>&1 && BUILDER="$b" && break; done
  [ -n "$BUILDER" ] || { echo "BUILD=1 needs docker, podman or nerdctl" >&2; exit 1; }
  # version:fault pairs, same as the GitHub workflow. The extra versions are only built for overlays that use them.
  BUILDS="${VERSION}:none"
  [ "${OVERLAY}" = "base" ] || BUILDS="${BUILDS} 1.2.0:errors 1.3.0:latency"
  for vf in ${BUILDS}; do
    v="${vf%%:*}"; f="${vf##*:}"
    echo "==> Building ${IMAGE_REPO}:${v} (FAULT_MODE=${f}) with ${BUILDER}"
    DOCKER_BUILDKIT=1 "$BUILDER" build --build-arg VERSION="${v}" --build-arg FAULT_MODE="${f}" \
      --build-arg DD_GIT_COMMIT_SHA="$(git rev-parse HEAD)" -t "${IMAGE_REPO}:${v}" .
    case "${CTX}" in
      kind-*)     kind load docker-image "${IMAGE_REPO}:${v}" --name "${CTX#kind-}" ;;
      minikube*)  minikube image load "${IMAGE_REPO}:${v}" ;;
      k3d-*)      k3d image import "${IMAGE_REPO}:${v}" -c "${CTX#k3d-}" ;;
      docker-desktop|rancher-desktop|colima*) echo "==> Local image store is shared, nothing to load" ;;
      *) echo "==> Unknown cluster type: push ${IMAGE_REPO}:${v} to a registry the cluster can pull from, then deploy with IMAGE=<registry>/<name>:<tag>" ;;
    esac
  done
  PULL_POLICY="IfNotPresent"
fi

kubectl get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl create ns "${NAMESPACE}"
kubectl kustomize "deploy/${OVERLAY}" \
  | sed -e "s|image: ${REGISTRY_REPO}:|image: ${IMAGE_REPO}:|" -e "s|imagePullPolicy: Always|imagePullPolicy: ${PULL_POLICY}|" \
  | kubectl apply -n "${NAMESPACE}" -f -
# Restart so the pods pick up the image and version label.
# Restart every Deployment this overlay defines, so pods pick up the image and version label.
DEPLOYS="$(kubectl kustomize "deploy/${OVERLAY}" | awk '/^kind: Deployment/{f=1} f&&/^  name:/{print $2; f=0}')"
for d in ${DEPLOYS}; do kubectl -n "${NAMESPACE}" rollout restart "deploy/${d}" >/dev/null; done
for d in ${DEPLOYS}; do
  kubectl -n "${NAMESPACE}" rollout status "deploy/${d}" --timeout=180s
done

cat <<MSG

Deployed. The load generator is already sending requests.
Open Datadog APM and filter:  env:lab  service:go-shop-*
Traces appear within about a minute.
MSG
