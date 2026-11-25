#!/usr/bin/env python3
"""Check that every Deployment has one consistent version.

For each Deployment rendered by `kubectl kustomize deploy/overlays/versions`, these must match:
  - metadata.labels["tags.datadoghq.com/version"]            (Deployment label)
  - spec.template.metadata.labels["tags.datadoghq.com/version"]   (pod label, becomes DD_VERSION)
  - the image tag
  - DD_VERSION read from that pod label (fieldRef), not hard-coded
Also checks env and service labels exist, as Unified Service Tagging requires.
Needs kubectl (offline is fine) and PyYAML (`pip install pyyaml`). Exit code 1 on any mismatch.
"""
import subprocess, sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

LABEL = "tags.datadoghq.com/version"
overlay = sys.argv[1] if len(sys.argv) > 1 else "deploy/overlays/versions"

rendered = subprocess.run(["kubectl", "kustomize", overlay], capture_output=True, text=True, check=True).stdout
items = [d for d in yaml.safe_load_all(rendered) if d]

bad, seen = [], []
for o in items:
    if o["kind"] != "Deployment":
        continue
    name = o["metadata"]["name"]
    dep_labels = o["metadata"].get("labels", {})
    pod = o["spec"]["template"]
    pod_labels = pod["metadata"].get("labels", {})
    c = pod["spec"]["containers"][0]
    tag = c["image"].rsplit(":", 1)[-1] if ":" in c["image"].split("/")[-1] else "(none)"
    env = {e["name"]: e for e in c.get("env", [])}
    v = pod_labels.get(LABEL)
    seen.append((name, v, tag))
    if dep_labels.get(LABEL) != v:
        bad.append(f"{name}: Deployment label {dep_labels.get(LABEL)!r} != pod label {v!r}")
    if tag != v:
        bad.append(f"{name}: image tag {tag!r} != version label {v!r}")
    ref = env.get("DD_VERSION", {}).get("valueFrom", {}).get("fieldRef", {}).get("fieldPath", "")
    if LABEL not in ref:
        bad.append(f"{name}: DD_VERSION must come from the pod label {LABEL}, got {env.get('DD_VERSION')}")
    for lbl in ("tags.datadoghq.com/env", "tags.datadoghq.com/service"):
        if lbl not in pod_labels or lbl not in dep_labels:
            bad.append(f"{name}: missing {lbl} on the Deployment or the pod template")

for name, v, tag in seen:
    print(f"  {name:18} version={v}  image tag={tag}")
if bad:
    print("FAIL")
    print("\n".join("  - " + b for b in bad))
    sys.exit(1)
print(f"OK: {len(seen)} Deployments have a consistent version")
