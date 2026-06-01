---
name: kmm-ordered-upgrade
description: >-
  Test the KMM ordered upgrade feature end-to-end: deploy a versioned Module
  with kmm_ci_a, upgrade to kmm_ci_b node-by-node, and verify the old module
  is unloaded and the new one loaded on each node. Use when the user asks to
  test ordered upgrade, module versioning, or node-by-node kmod rollout.
---
# KMM Ordered Upgrade E2E Test

Tests the ordered upgrade flow: deploy a versioned kernel module (`kmm_ci_a`), then upgrade it node-by-node to a different module (`kmm_ci_b`) without rebooting. The swap is verified via `lsmod` — after upgrade, `kmm_ci_b` must be loaded and `kmm_ci_a` must not.

All commands assume you are in the repository root. This skill directory contains supporting files:

- `Dockerfile.v1` — image with `kmm_ci_a.ko` only
- `Dockerfile.v2` — image with `kmm_ci_b.ko` only
- `module-v1.yaml` — Module CR template (version "1", `moduleName: kmm_ci_a`)
- `module-v2.yaml` — Module CR for v2 (version "2", `moduleName: kmm_ci_b`)

## Step 1: Ensure a running KMM environment

Use the **kmm-dev-setup** skill to ensure a Minikube cluster is running with KMM deployed. The cluster must use `--driver=docker` and have the local registry (`kmm-registry`) running.

```bash
kubectl wait --timeout=5m --for=condition=Available deployment/kmm-operator-controller deployment/kmm-operator-webhook -n kmm-operator-system
docker ps --filter name=kmm-registry --format '{{.Names}}'
```

## Step 2: Build the kernel modules locally

Prerequisites: `gcc`, `make`, kernel headers (`/lib/modules/$(uname -r)/build`).

```bash
(cd ci/kmm-kmod && make all)
```

Verify both `kmm_ci_a.ko` and `kmm_ci_b.ko` exist in `ci/kmm-kmod/`.

## Step 3: Build and push v1 and v2 images

v1 contains only `kmm_ci_a.ko`, v2 contains only `kmm_ci_b.ko`.

```bash
KERNEL_VER=$(minikube ssh -- uname -r | tr -d '\r')
export V1_TAG="v1-$(date +%s)"
export V2_TAG="v2-$(date +%s)"
SKILL_DIR=.agents/skills/kmm-ordered-upgrade

docker build --build-arg KERNEL_VERSION=$KERNEL_VER \
  -t localhost:5000/kmm-kmod-base:$V1_TAG -f $SKILL_DIR/Dockerfile.v1 ci/kmm-kmod/

docker build --build-arg KERNEL_VERSION=$KERNEL_VER \
  -t localhost:5000/kmm-kmod-base:$V2_TAG -f $SKILL_DIR/Dockerfile.v2 ci/kmm-kmod/

docker push localhost:5000/kmm-kmod-base:$V1_TAG
docker push localhost:5000/kmm-kmod-base:$V2_TAG
```

Save `V1_TAG` and `V2_TAG` — they are needed in steps 4 and 6.

## Step 4: Deploy the v1 Module

Label workers, create build/sign resources, and apply the Module CR from `module-v1.yaml`.

```bash
kubectl label node minikube-m02 minikube-m03 kmm-test/role=worker --overwrite

kubectl create secret generic build-secret \
  --from-literal=ci-build-secret=super-secret-value \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl create configmap kmm-kmod-dockerfile \
  --from-literal=dockerfile="$(cat <<EOF
FROM host.minikube.internal:5000/kmm-kmod-base:$V1_TAG
RUN grep super-secret-value /run/secrets/build-secret/ci-build-secret
EOF
)" --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f ci/secret_kmm-kmod-signing.yaml
kubectl delete module kmm-ci --ignore-not-found

envsubst '$V1_TAG' < $SKILL_DIR/module-v1.yaml | kubectl apply -f -
```

Module is created with `version: "1"` but no nodes have the version label yet — nothing loads.

## Step 5: Label nodes to trigger v1 load

```bash
for NODE in minikube-m02 minikube-m03; do
  kubectl label node $NODE kmm.node.kubernetes.io/version-module.default.kmm-ci=1
done
```

Wait for `version.ready=1` on both nodes (build-sign-load pipeline must complete first, may take a few minutes):

```bash
for NODE in minikube-m02 minikube-m03; do
  echo "Waiting for v1 ready on $NODE..."
  until kubectl get node "$NODE" -o jsonpath='{.metadata.labels.kmm\.node\.kubernetes\.io/default\.kmm-ci\.version\.ready}' 2>/dev/null | grep -q "1"; do
    sleep 5
  done
  echo "$NODE: v1 ready"
done
```

Verify `kmm_ci_a` is loaded and `kmm_ci_b` is **not**:

```bash
minikube ssh -n minikube-m02 -- lsmod | grep kmm_ci
minikube ssh -n minikube-m03 -- lsmod | grep kmm_ci
```

Check `version.ready` shows `1`:

```bash
kubectl get nodes -l kmm-test/role=worker -L kmm.node.kubernetes.io/default.kmm-ci.version.ready
```

## Step 6: Upgrade the Module to v2

Update the Dockerfile ConfigMap and apply the v2 Module CR. The `moduleName` changes from `kmm_ci_a` to `kmm_ci_b`.

```bash
kubectl create configmap kmm-kmod-dockerfile \
  --from-literal=dockerfile="$(cat <<EOF
FROM host.minikube.internal:5000/kmm-kmod-base:$V2_TAG
RUN grep super-secret-value /run/secrets/build-secret/ci-build-secret
EOF
)" --dry-run=client -o yaml | kubectl apply -f -

envsubst '$V2_TAG' < $SKILL_DIR/module-v2.yaml | kubectl apply -f -
```

Nodes still run v1 — the version-module labels haven't changed yet.

Verify `kmm_ci_a` is still loaded:

```bash
minikube ssh -n minikube-m02 -- lsmod | grep kmm_ci
```

## Step 7: Ordered node-by-node upgrade

Upgrade one node at a time. After each label change, wait for `version.ready` to show `2`, then verify `kmm_ci_b` is loaded and `kmm_ci_a` is not.

**First node:**

```bash
kubectl label node minikube-m02 kmm.node.kubernetes.io/version-module.default.kmm-ci=2 --overwrite

echo "Waiting for v2 ready on minikube-m02..."
until kubectl get node minikube-m02 -o jsonpath='{.metadata.labels.kmm\.node\.kubernetes\.io/default\.kmm-ci\.version\.ready}' 2>/dev/null | grep -q "2"; do
  sleep 5
done
```

Verify the swap:

```bash
minikube ssh -n minikube-m02 -- lsmod | grep kmm_ci
kubectl get nodes -l kmm-test/role=worker -L kmm.node.kubernetes.io/default.kmm-ci.version.ready
```

At this point minikube-m02 has v2 (`kmm_ci_b`) and minikube-m03 still has v1 (`kmm_ci_a`).

**Second node:**

```bash
kubectl label node minikube-m03 kmm.node.kubernetes.io/version-module.default.kmm-ci=2 --overwrite

echo "Waiting for v2 ready on minikube-m03..."
until kubectl get node minikube-m03 -o jsonpath='{.metadata.labels.kmm\.node\.kubernetes\.io/default\.kmm-ci\.version\.ready}' 2>/dev/null | grep -q "2"; do
  sleep 5
done
```

Verify:

```bash
minikube ssh -n minikube-m03 -- lsmod | grep kmm_ci
kubectl get nodes -l kmm-test/role=worker -L kmm.node.kubernetes.io/default.kmm-ci.version.ready
```

Both nodes should now show `version.ready=2` and only `kmm_ci_b` in `lsmod`.

## Cleanup

```bash
kubectl label node minikube-m02 minikube-m03 kmm.node.kubernetes.io/version-module.default.kmm-ci-
kubectl delete module kmm-ci --ignore-not-found
kubectl delete secret build-secret kmm-kmod-signing-cert kmm-kmod-signing-key --ignore-not-found
kubectl delete configmap kmm-kmod-dockerfile --ignore-not-found
kubectl label node minikube-m02 minikube-m03 kmm-test/role-
(cd ci/kmm-kmod && make -C /lib/modules/$(uname -r)/build M=$(pwd) clean)
```

## Troubleshooting

If a node gets stuck during upgrade:

```bash
kubectl get node <node> --show-labels | tr ',' '\n' | grep -E "version|kmm"
kubectl get nodemodulesconfig <node> -o yaml
kubectl get pods -A -l kmm.node.kubernetes.io/module.name=kmm-ci --field-selector spec.nodeName=<node>
kubectl logs -n kmm-operator-system deployment/kmm-operator-controller --tail=100
```
