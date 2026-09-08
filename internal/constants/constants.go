package constants

const (
	ModuleNameLabel        = "kmm.node.kubernetes.io/module.name"
	ModuleNamespaceLabel   = "kmm.node.kubernetes.io/module.namespace"
	NodeLabelerFinalizer   = "kmm.node.kubernetes.io/node-labeler"
	TargetKernelTarget     = "kmm.node.kubernetes.io/target-kernel"
	ResourceType           = "kmm.node.kubernetes.io/resource-type"
	ResourceHashAnnotation = "kmm.node.kubernetes.io/last-hash"
	KernelLabel            = "kmm.node.kubernetes.io/kernel-version.full"
	DaemonSetRole          = "kmm.node.kubernetes.io/role"
	NamespaceLabelKey      = "kmm.node.k8s.io/contains-modules"

	WorkerPodVersionLabelPrefix   = "beta.kmm.node.kubernetes.io/version-worker-pod"
	SchedulePodVersionLabelPrefix = "beta.kmm.node.kubernetes.io/version-schedule-pod"
	ModuleVersionLabelPrefix      = "kmm.node.kubernetes.io/version-module"

	GCDelayFinalizer  = "kmm.node.kubernetes.io/gc-delay"
	ModuleFinalizer   = "kmm.node.kubernetes.io/module-finalizer"
	JobEventFinalizer = "kmm.node.kubernetes.io/job-event-finalizer"

	// The DRA reconciler holds the Module itself, because ModuleFinalizer is released as soon as no
	// NMC lists the Module as in use, which says nothing about whether the cleanup has finished.
	DRAFinalizer = "kmm.node.kubernetes.io/dra-cleanup"

	ManagedClusterModuleNameLabel  = "kmm.node.kubernetes.io/managedclustermodule.name"
	KernelVersionsClusterClaimName = "kernel-versions.kmm.node.kubernetes.io"
	DockerfileCMKey                = "dockerfile"
	PublicSignDataKey              = "cert"
	PrivateSignDataKey             = "key"

	ModuleLoaderRoleLabelValue = "module-loader"
	DevicePluginRoleLabelValue = "device-plugin"
	DRARoleLabelValue          = "dra"

	OperatorNamespaceEnvVar = "OPERATOR_NAMESPACE"

	MinKubeMajorForDRA = 1
	MinKubeMinorForDRA = 34
)
