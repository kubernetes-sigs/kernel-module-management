---
name: kmm-dev-setup
description: >-
  Set up a local KMM (Kernel Module Management) dev environment from scratch.
  Use when the user asks to create, rebuild, redeploy, or tear down a KMM
  development environment, or when you need a running KMM instance to test changes.
---
# KMM Dev Environment Setup

All commands below assume you are in the repository root.

Follow these steps to get a working KMM instance running your current code.

## Step 1: Create a multi-node Minikube cluster with a local registry

Check if a cluster already exists:

```bash
minikube status
```

- If the cluster is running **and** was created with `--driver=docker --nodes=3`, skip to Step 2.
- If the cluster exists but uses the wrong driver or node count, delete it first with `minikube delete`.
- If no cluster exists, create one:

Start the local registry (removes any leftover container first):

```bash
docker rm -f kmm-registry 2>/dev/null || true
docker run -d -p 5000:5000 --name kmm-registry registry:2
```

Create a 3-node cluster (1 control-plane + 2 workers) using the **Docker driver**:

```bash
minikube start --nodes=3 --driver=docker --insecure-registry=host.minikube.internal:5000
```

> **Important**: Always use `--driver=docker`. The Podman driver has broken pod DNS (pods cannot resolve external names like `gcr.io`), which breaks the Kaniko build/sign pipeline.

Configure CRI-O on all nodes to trust the insecure registry (the `--insecure-registry` flag alone does not configure CRI-O):

```bash
for node in minikube minikube-m02 minikube-m03; do
  minikube ssh -n $node -- sudo bash -c '"mkdir -p /etc/containers/registries.conf.d && cat > /etc/containers/registries.conf.d/host-minikube.conf <<EOF
[[registry]]
location = \"host.minikube.internal:5000\"
insecure = true
EOF"'
  minikube ssh -n $node -- sudo systemctl restart crio
done
```

To destroy the cluster when done:

```bash
minikube delete
docker rm -f kmm-registry
```

## Step 2: Build images from current code

Generate a unique tag and build only the images affected by your changes:

```bash
TAG=$(date +%Y%m%d%H%M%S)
```

Build the controller image (if controller code changed):

```bash
make docker-build IMG=kmm-controller:$TAG
```

Build the webhook image (if webhook code changed):

```bash
make webhookimage-build WEBHOOK_IMG=kmm-webhook:$TAG
```

Only rebuild images whose code you actually modified. Use a **unique tag each time** you rebuild. If you reuse the same tag, the cluster nodes will use the cached image and ignore the new one.

## Step 3: Load images into the cluster

```bash
minikube image load kmm-controller:$TAG
minikube image load kmm-webhook:$TAG
```

This distributes the images to all nodes in the cluster.

Verify the images are available:

```bash
minikube image list | grep kmm-controller
```

## Step 4: Deploy KMM

```bash
make deploy \
  IMG=localhost/kmm-controller:$TAG \
  WEBHOOK_IMG=localhost/kmm-webhook:$TAG
```

This installs cert-manager (required dependency), generates CRDs/RBAC/webhook manifests, and applies everything to the cluster via kustomize.

Note: `make deploy` edits kustomization files under `config/` to set image references. These changes show up in `git status` but should not be committed.

## Redeploy after code changes

Set a new `TAG=$(date +%Y%m%d%H%M%S)` and repeat steps 2-4.

## Tear down

```bash
make undeploy
```

This also removes cert-manager.
