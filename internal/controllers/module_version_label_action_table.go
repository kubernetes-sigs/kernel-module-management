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
	module         string
	workerPod      string
	schedulePlugin string
}

type labelAction struct {
	getLabelName func(string, string) string
	action       string
}

var labelActionTable = map[labelActionKey]labelAction{
	{
		module:         labelMissing,
		workerPod:      labelMissing,
		schedulePlugin: labelMissing}: {getLabelName: nil, action: noneAction},

	{
		module:         labelMissing,
		workerPod:      labelPresent,
		schedulePlugin: labelPresent}: {getLabelName: utils.GetSchedulePluginVersionLabelName, action: deleteAction},

	{
		module:         labelMissing,
		workerPod:      labelPresent,
		schedulePlugin: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},

	{
		module:         labelPresent,
		workerPod:      labelMissing,
		schedulePlugin: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: addAction},

	{
		module:         labelPresent,
		workerPod:      labelPresent,
		schedulePlugin: labelMissing}: {getLabelName: utils.GetSchedulePluginVersionLabelName, action: addAction},

	{
		module:         labelPresent,
		workerPod:      labelPresent,
		schedulePlugin: labelPresent}: {getLabelName: nil, action: noneAction},

	{
		module:         labelPresent,
		workerPod:      labelDifferent,
		schedulePlugin: labelDifferent}: {getLabelName: utils.GetSchedulePluginVersionLabelName, action: deleteAction},

	{
		module:         labelPresent,
		workerPod:      labelDifferent,
		schedulePlugin: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},
}
