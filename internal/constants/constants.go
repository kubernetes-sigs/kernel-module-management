package constants

const (
	ModuleNameLabel      = "kmm.node.kubernetes.io/module.name"
	NodeLabelerFinalizer = "kmm.node.kubernetes.io/node-labeler"
	TargetKernelTarget   = "kmm.node.kubernetes.io/target-kernel"
	DaemonSetRole        = "kmm.node.kubernetes.io/role"
	JobType              = "kmm.node.kubernetes.io/job-type"
	KernelLabel          = "kmm.node.kubernetes.io/kernel-version.full"

	ModuleLoaderVersionLabelPrefix = "beta.kmm.node.kubernetes.io/version-module-loader"
	DevicePluginVersionLabelPrefix = "beta.kmm.node.kubernetes.io/version-device-plugin"
	ModuleVersionLabelPrefix       = "kmm.node.kubernetes.io/version-module"

	ManagedClusterModuleNameLabel  = "kmm.node.kubernetes.io/managedclustermodule.name"
	KernelVersionsClusterClaimName = "kernel-versions.kmm.node.kubernetes.io"
	DockerfileCMKey                = "dockerfile"
	PublicSignDataKey              = "cert"
	PrivateSignDataKey             = "key"

	BuildImageEnvVar        = "RELATED_IMAGES_BUILD"
	OperatorNamespaceEnvVar = "OPERATOR_NAMESPACE"
	SignImageEnvVar         = "RELATED_IMAGES_SIGN"
)
