package worker

const (
	FlagFirmwareClassPath = "set-firmware-class-path"

	FirmwareClassPathLocation = "/sys/module/firmware_class/parameters/path"
	ImagesDir                 = "/var/run/kmm/images"
	PullSecretsDir            = "/var/run/kmm/pull-secrets"
)
