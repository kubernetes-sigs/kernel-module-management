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
	moduleLoader string
	devicePlugin string
}

type labelAction struct {
	getLabelName func(string, string) string
	action       string
}

var labelActionTable = map[labelActionKey]labelAction{
	labelActionKey{
		module:       labelMissing,
		moduleLoader: labelMissing,
		devicePlugin: labelMissing}: labelAction{getLabelName: nil, action: noneAction},

	labelActionKey{
		module:       labelMissing,
		moduleLoader: labelPresent,
		devicePlugin: labelPresent}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelMissing,
		moduleLoader: labelPresent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetModuleLoaderVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelPresent,
		moduleLoader: labelMissing,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetModuleLoaderVersionLabelName, action: addAction},

	labelActionKey{
		module:       labelPresent,
		moduleLoader: labelPresent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: addAction},

	labelActionKey{
		module:       labelPresent,
		moduleLoader: labelPresent,
		devicePlugin: labelPresent}: labelAction{getLabelName: nil, action: noneAction},

	labelActionKey{
		module:       labelPresent,
		moduleLoader: labelDifferent,
		devicePlugin: labelDifferent}: labelAction{getLabelName: utils.GetDevicePluginVersionLabelName, action: deleteAction},

	labelActionKey{
		module:       labelPresent,
		moduleLoader: labelDifferent,
		devicePlugin: labelMissing}: labelAction{getLabelName: utils.GetModuleLoaderVersionLabelName, action: deleteAction},
}
