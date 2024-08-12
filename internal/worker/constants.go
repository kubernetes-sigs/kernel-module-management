package worker

const (
	FlagFirmwarePath = "firmware-path"

	FirmwareClassPathLocation = "/sys/module/firmware_class/parameters/path"
	ImagesDir                 = "/var/run/kmm/images"
	PullSecretsDir            = "/var/run/kmm/pull-secrets"
)
