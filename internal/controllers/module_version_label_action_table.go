package controllers

import "github.com/kubernetes-sigs/kernel-module-management/internal/utils"

const (
	labelMissing   = "missing"
	labelPresent   = "present"
	labelDifferent = "different"

	addAction    = "add"
	deleteAction = "delete"
	noneAction   = "none"
)

type labelActionKey struct {
	module       string
	workerPod    string
	devicePlugin string
}

type labelAction struct {
	getLabelName func(string, string) string
	action       string
}

var labelActionTable = map[labelActionKey]labelAction{
	labelActionKey{
		module:       labelMissing,
		workerPod:    labelMissing,
		devicePlugin: labelMissing}: labelAction{getLabelName: nil, action: noneAction},

	labelActionKey{
		module:       labelMissing,
		workerPod:    labelPresent,
		devicePlugin: labelPresent}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelMissing,
		workerPod:    labelPresent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelPresent,
		workerPod:    labelMissing,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetWorkerPodVersionLabelName, action: addAction},

	labelActionKey{
		module:       labelPresent,
		workerPod:    labelPresent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: addAction},

	labelActionKey{
		module:       labelPresent,
		workerPod:    labelPresent,
		devicePlugin: labelPresent}: labelAction{getLabelName: nil, action: noneAction},

	labelActionKey{
		module:       labelPresent,
		workerPod:    labelDifferent,
		devicePlugin: labelDifferent}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelPresent,
		workerPod:    labelDifferent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},
}
