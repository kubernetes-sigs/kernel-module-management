---
name: kmm-e2e
description: >-
  Run a manual e2e test for KMM: deploy the operator, create build/sign resources,
  apply a Module, and verify the kernel module loads on the node. Use when the user
  asks to run an e2e test, verify a kernel module loads, or test the full
  build-sign-load pipeline.
---
# KMM E2E Test

This skill walks through a manual e2e test of the KMM build-sign-load pipeline on a Minikube cluster. It builds kernel modules on the host (where headers already exist) and uses a trivial Dockerfile for the in-cluster build.

All commands assume you are in the repository root. This skill directory contains:

- `Dockerfile.base` — base image with both `kmm_ci_a.ko` and `kmm_ci_b.ko`
- `module.yaml` — Module CR template; `$BASE_TAG` is substituted at apply time via `envsubst`

## Step 1: Ensure a running KMM environment

Use the **kmm-dev-setup** skill to ensure a Minikube cluster is running with KMM deployed from the current code. The cluster must use `--driver=docker` and have the local registry (`kmm-registry`) running.

Verify both deployments are available:

```bash
kubectl wait --for=condition=Available deployment/kmm-operator-controller deployment/kmm-operator-webhook -n kmm-operator-system
```

## Step 2: Verify the local registry is running

The **kmm-dev-setup** skill creates a `kmm-registry` container. Verify it is running:

```bash
docker ps --filter name=kmm-registry --format '{{.Names}}'
```

If it is not running, start it:

```bash
docker rm -f kmm-registry 2>/dev/null || true
docker run -d -p 5000:5000 --name kmm-registry registry:2
```

## Step 3: Build the kernel modules locally

The `.ko` files must be built for the same kernel running on the Minikube nodes. With the Docker driver, Minikube nodes share the host kernel, so building on the host produces compatible modules.

Prerequisites: `gcc`, `make`, and kernel headers on the host (`/lib/modules/$(uname -r)/build` must exist).

Build from inside the `ci/kmm-kmod` directory:

```bash
(cd ci/kmm-kmod && make all)
```

Verify `kmm_ci_a.ko` and `kmm_ci_b.ko` were created.

## Step 4: Build images, create resources, and apply the Module

This step must run as a single script because `$BASE_TAG` is used for the base image tag, the Dockerfile ConfigMap, and the Module image name.

```bash
KERNEL_VER=$(minikube ssh -- uname -r | tr -d '\r')
export BASE_TAG=$(date +%s)
SKILL_DIR=.agents/skills/kmm-e2e

docker build --build-arg KERNEL_VERSION=$KERNEL_VER \
  -t localhost:5000/kmm-kmod-base:$BASE_TAG -f $SKILL_DIR/Dockerfile.base ci/kmm-kmod/

docker push localhost:5000/kmm-kmod-base:$BASE_TAG

kubectl create secret generic build-secret \
  --from-literal=ci-build-secret=super-secret-value \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl create configmap kmm-kmod-dockerfile \
  --from-literal=dockerfile="$(cat <<EOF
FROM host.minikube.internal:5000/kmm-kmod-base:$BASE_TAG
RUN grep super-secret-value /run/secrets/build-secret/ci-build-secret
EOF
)" --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f ci/secret_kmm-kmod-signing.yaml

envsubst '$BASE_TAG' < $SKILL_DIR/module.yaml | kubectl apply -f -
```

The `envsubst` substitutes only `$BASE_TAG` in the Module template — KMM handles `$MOD_NAMESPACE`, `$MOD_NAME`, and `$KERNEL_FULL_VERSION` internally at runtime.

This triggers the full pipeline: **build pod -> sign pod -> worker pod**. Wait for the module to be ready:

```bash
echo "Waiting for module ready on minikube-m02..."
until kubectl get node minikube-m02 -o jsonpath='{.metadata.labels}' 2>/dev/null | grep -q "kmm.node.kubernetes.io/default.kmm-ci.ready"; do
  sleep 5
done
echo "Module ready"
```

If it takes longer than 3 minutes, check pod status and logs:

```bash
kubectl get pods -l kmm.node.kubernetes.io/module.name=kmm-ci
kubectl logs -l kmm.node.kubernetes.io/module.name=kmm-ci --prefix
```

## Step 5: Verify the module is loaded on the target worker

Check `minikube-m02` (the node selected in Step 4):

```bash
minikube ssh -n minikube-m02 -- lsmod | grep kmm_ci
```

Both `kmm_ci_a` and `kmm_ci_b` should appear. If either is missing, the test failed.

## Cleanup

Remove the Module (triggers module unload):

```bash
kubectl delete module --all --wait=false
```

Verify modules are unloaded:

```bash
minikube ssh -n minikube-m02 -- lsmod | grep kmm_ci  # should return nothing
```

Clean up resources:

```bash
kubectl delete secret build-secret kmm-kmod-signing-cert kmm-kmod-signing-key --ignore-not-found
kubectl delete configmap kmm-kmod-dockerfile --ignore-not-found
```

Clean up locally-built .ko files:

```bash
(cd ci/kmm-kmod && make -C /lib/modules/$(uname -r)/build M=$(pwd) clean)
```

Optionally stop the registry:

```bash
docker rm -f kmm-registry
```
