#!/usr/bin/env bash
# Set the app version in one place. Updates VERSION, the image tag and the
# tags.datadoghq.com/version label (which becomes DD_VERSION) in every manifest.
# Usage: ./scripts/set-version.sh 1.1.0
set -euo pipefail
cd "$(dirname "$0")/.."
NEW="${1:?usage: set-version.sh MAJOR.MINOR.PATCH}"
[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version must look like 1.2.3" >&2; exit 1; }
OLD="$(tr -d '[:space:]' < VERSION)"
echo "$NEW" > VERSION
for f in deploy/base/shop-*.yaml; do
  sed -i.bak -e "s|storedog-go-apm-example:${OLD}|storedog-go-apm-example:${NEW}|" \
             -e "s|tags.datadoghq.com/version: \"${OLD}\"|tags.datadoghq.com/version: \"${NEW}\"|" "$f"
  rm -f "$f.bak"
done
echo "Version ${OLD} -> ${NEW}"
echo "Next: git commit, push (the image workflow builds ghcr.io/datadog-asean-se/storedog-go-apm-example:${NEW}), then: git tag v${NEW} && git push --tags"
