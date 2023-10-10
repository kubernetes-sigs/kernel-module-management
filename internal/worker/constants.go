package worker

const (
	FlagFirmwareClassPath = "set-firmware-class-path"
	FlagFirmwareMountPath = "set-firmware-mount-path"

	FirmwareClassPathLocation = "/sys/module/firmware_class/parameters/path"
	ImagesDir                 = "/var/run/kmm/images"
	PullSecretsDir            = "/var/run/kmm/pull-secrets"
	FirmwareMountPath         = "/var/lib/firmware"
)
