# Hub-Spoke deployment

Using [Open Cluster Management](https://open-cluster-management.io/), allows to configure and define policies in our control cluster, named `hub` that later can be applied to satellite clusters named `spoke`.

We can leverage OCM to deploy KMM on the environment and have the different components orchestrated by OCM.

!!! note "Using podman instead of docker"

    If you're using podman like we'll do, we need to symlink the command to the `docker` name, as the `Makefile` does uses docker.

    `ln -s /usr/bin/podman /usr/bin/docker`

First we need to prepare our registry, if you're using <https://quay.io>, remember to change repositories created to public if those were not created beforehand once you've pushed to them or you won't be able to consume the images.

## Dependencies

- `registry.ci.openshift.org` token - [Documentation](https://docs.ci.openshift.org/docs/how-tos/use-registries-in-build-farm/#how-do-i-log-in-to-pull-images-that-require-authentication)

!!! warning "`quay.io` account"

    if the repositories are not created beforehand, please make sure to convert them to public ones after pushing to them

## Setup the Hub

1. Build and push the KMM-Hub container image, the KMM-Hub OLM bundle and catalog from the midstream repository:

```bash
MYUSER=${YOURREGISTRYUSERNAME}
REGISTRY=quay.io

git clone git@github.com:rh-ecosystem-edge/kernel-module-management.git
cd kernel-module-management

# Build KMM-Hub container image

HUB_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-hub:latest make docker-build-hub

# Push KMM-Hub container image

HUB_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-hub:latest make docker-push-hub

# Generate KMM-Hub OLM bundle

HUB_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-hub:latest BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle-hub make bundle-hub

# Build KMM-Hub OLM bundle container

BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle-hub make bundle-build-hub

# Push KMM-Hub OLM bundle container

BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle-hub make bundle-push

# Build KMM-Hub OLM catalog

CATALOG_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-catalog-hub:latest BUNDLE_IMGS=${REGISTRY}/${MYUSER}/kernel-module-management-bundle-hub make catalog-build

# Push KMM-Hub OLM catalog

CATALOG_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-catalog-hub:latest make catalog-push
```

2. Deploy the KMM-Hub OLM catalog on the Hub:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
    name: kmm-hub-catalog
    namespace: openshift-marketplace
spec:
    sourceType: grpc
    image: ${REGISTRY}/${MYUSER}/kernel-module-management-catalog-hub:latest
    displayName: KMM Hub Catalog
    publisher: ${MYUSER}
    updateStrategy:
    registryPoll:
        interval: 5m
EOF
```

3. Deploy the KMM-Hub operator by creating an OLM Subscription:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
    name: kmm-operator
    namespace: openshift-operators
spec:
    channel: alpha
    installPlanApproval: Automatic
    name: kernel-module-management-hub
    source: kmm-hub-catalog
    sourceNamespace: openshift-marketplace
EOF
```

4. Deploy the ACM Policy that adds the required permissions to the Spoke klusterlet, in order for the latter to be able to CRUD KMM Module CRs:

```bash
cat <<EOF | kubectl apply -f -
---
apiVersion: policy.open-cluster-management.io/v1
kind: Policy
metadata:
    name: allow-klusterlet-deploy-kmm-modules
spec:
    remediationAction: enforce
    disabled: false
    policy-templates:
    - objectDefinition:
        apiVersion: policy.open-cluster-management.io/v1
        kind: ConfigurationPolicy
        metadata:
            name: klusterlet-deploy-modules
        spec:
            severity: high
            object-templates:
            - complianceType: mustonlyhave
            objectDefinition:
                apiVersion: rbac.authorization.k8s.io/v1
                kind: ClusterRole
                metadata:
                name: kmm-module-manager
                rules:
                - apiGroups: [kmm.sigs.x-k8s.io]
                    resources: [modules]
                    verbs: [create, delete, get, list, patch, update, watch]
            - complianceType: mustonlyhave
            objectDefinition:
                apiVersion: rbac.authorization.k8s.io/v1
                kind: RoleBinding
                metadata:
                name: klusterlet-kmm
                namespace: open-cluster-management-agent
                subjects:
                - kind: ServiceAccount
                name: klusterlet-work-sa
                namespace: open-cluster-management-agent
                roleRef:
                kind: ClusterRole
                name: kmm-module-manager
                apiGroup: rbac.authorization.k8s.io
---
apiVersion: apps.open-cluster-management.io/v1
kind: PlacementRule
metadata:
    name: all-clusters-except-local
spec:
    clusterSelector:
    matchExpressions:
        - key: name
        operator: NotIn
        values:
            - local-cluster
---
apiVersion: policy.open-cluster-management.io/v1
kind: PlacementBinding
metadata:
    name: bind-klusterlet-kmm-all-clusters
placementRef:
    apiGroup: apps.open-cluster-management.io
    kind: PlacementRule
    name: all-clusters-except-local
subjects:
    - apiGroup: policy.open-cluster-management.io
    kind: Policy
    name: allow-klusterlet-deploy-kmm-modules
EOF
```

5. Create a namespace on the Hub, in which the Build and Sign jobs will be created:

```bash
kubectl create ns kmm-jobs-test
```

## Setup the Spoke

1. Build and push the KMM container image, the KMM OLM bundle and catalog from midstream repository:

```bash
# Build KMM container image
IMG=${REGISTRY}/${MYUSER}/kernel-module-management:latest make docker-build

# Push KMM container image
IMG=${REGISTRY}/${MYUSER}/kernel-module-management:latest make docker-push

# Generate KMM OLM bundle
IMG=${REGISTRY}/${MYUSER}/kernel-module-management:latest BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle make bundle

# Build KMM OLM bundle container
BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle make bundle-build

# Push KMM OLM bundle container
BUNDLE_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-bundle make bundle-push

# Build KMM OLM catalog
CATALOG_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-catalog:latest BUNDLE_IMGS=${REGISTRY}/${MYUSER}/kernel-module-management-bundle make catalog-build

# Push KMM OLM catalog
CATALOG_IMG=${REGISTRY}/${MYUSER}/kernel-module-management-catalog:latest make catalog-push
```

2. Deploy the KMM catalog on the Spoke:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
    name: kmm-catalog
    namespace: openshift-marketplace
spec:
    sourceType: grpc
    image: ${REGISTRY}/${MYUSER}/kernel-module-management-catalog:latest
    displayName: KMM Catalog
    publisher: ${MYUSER}
    updateStrategy:
    registryPoll:
        interval: 5m
EOF
```

3. Deploy the KMM operator by creating an OLM Subscription:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
    name: kernel-module-management
    namespace: openshift-operators
spec:
    channel: alpha
    config:
    env:
    - name: KMM_MANAGED
        value: "1"
    installPlanApproval: Automatic
    name: kernel-module-management
    source: kmm-catalog
    sourceNamespace: openshift-marketplace
EOF
```

4. Create a namespace on the Spoke, in which the `Module` will be created

```bash
kubectl create ns kmm-tests
```

## Create a ManagedClusterModule on the Hub

The following `ManagedClusteModule` includes a selector for a newly created `test` clusterset, which has only the spoke cluster assigned to it.

!!! warning

    In this example, I have created manually a docker secret in order to allow KMM-Hub to push the newly generated module container image to my [quay.io](http://quay.io) repository. For example:

    `MYUSER=${MYUSER}; MYBASE64PASSWORD="XXX"; kubectl create secret docker-registry -n kmm-jobs-test ${MYUSER}-quay-cred --docker-server=${REGISTRY}/${MYUSER} --docker-username=${MYUSER} --docker-password=${MYBASE64PASSWORD}`

!!! note "Cluster selector"

    Pay attention to the cluster selector in the example below:

    `cluster.open-cluster-management.io/clusterset: default`

    So that it targets one of your clusters.

1. Deploy a ManagedClusterModule to test the setup e2e:

   And now, the manifest itself:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
    name: mod-example
    namespace: kmm-jobs-test
data:
    dockerfile: |
    FROM image-registry.openshift-image-registry.svc:5000/openshift/driver-toolkit as builder
    ARG KERNEL_VERSION
    ARG MY_MODULE
    WORKDIR /build
    RUN git clone https://github.com/cdvultur/kmm-kmod.git
    WORKDIR /build/kmm-kmod
    RUN cp kmm_ci_a.c mod-example.c
    RUN make

    FROM registry.redhat.io/ubi8/ubi-minimal
    ARG KERNEL_VERSION
    ARG MY_MODULE
    RUN microdnf -y install kmod
    COPY --from=builder /usr/bin/kmod /usr/bin/
    RUN for link in /usr/bin/modprobe /usr/bin/rmmod; do \
        ln -s /usr/bin/kmod "\$link"; done
    COPY --from=builder /etc/driver-toolkit-release.json /etc/
    COPY --from=builder /build/kmm-kmod/*.ko /opt/lib/modules/\${KERNEL_VERSION}/
    RUN depmod -b /opt \${KERNEL_VERSION}
---
apiVersion: hub.kmm.sigs.x-k8s.io/v1beta1
kind: ManagedClusterModule
metadata:
    name: mod-example
spec:
    spokeNamespace: kmm-tests
    jobNamespace: kmm-jobs-test
    selector:
    cluster.open-cluster-management.io/clusterset: default
    moduleSpec:
    imageRepoSecret:
        name: ${MYUSER}-quay-cred
    moduleLoader:
        container:
        modprobe:
            moduleName: mod-example
        imagePullPolicy: Always
        kernelMappings:
        - regexp: '^.+$'
            containerImage: ${REGISTRY}/${MYUSER}/module:\${KERNEL_FULL_VERSION}
            pull:
            insecure: true
            insecureSkipTLSVerify: true
            build:
            buildArgs:
                - name: MY_MODULE
                value: mod-example.o
            dockerfileConfigMap:
                name: mod-example
    selector:
        node-role.kubernetes.io/worker: ""
EOF
```

2. Check the respective build on the Hub:

```bash
kubectl get builds -n kmm-jobs-test
```

3. After the build is completed, the respective ManifestWork will be created:

```bash
kubectl get manifestworks -n <spoke-cluster-name>
```

4. Check whether the KMM Module is created on the Spoke:

```bash
kubectl get modules -n kmm-tests
```

5. Check the KMM ModuleLoader DaemonSet on the Spoke:

```bash
kubectl get ds -n kmm-tests
```
