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
	module      string
	workerPod   string
	schedulePod string
}

type labelAction struct {
	getLabelName func(string, string) string
	action       string
}

var labelActionTable = map[labelActionKey]labelAction{
	{
		module:      labelMissing,
		workerPod:   labelMissing,
		schedulePod: labelMissing}: {getLabelName: nil, action: noneAction},

	{
		module:      labelMissing,
		workerPod:   labelPresent,
		schedulePod: labelPresent}: {getLabelName: utils.GetSchedulePodVersionLabelName, action: deleteAction},

	{
		module:      labelMissing,
		workerPod:   labelPresent,
		schedulePod: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},

	{
		module:      labelPresent,
		workerPod:   labelMissing,
		schedulePod: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: addAction},

	{
		module:      labelPresent,
		workerPod:   labelPresent,
		schedulePod: labelMissing}: {getLabelName: utils.GetSchedulePodVersionLabelName, action: addAction},

	{
		module:      labelPresent,
		workerPod:   labelPresent,
		schedulePod: labelPresent}: {getLabelName: nil, action: noneAction},

	{
		module:      labelPresent,
		workerPod:   labelDifferent,
		schedulePod: labelDifferent}: {getLabelName: utils.GetSchedulePodVersionLabelName, action: deleteAction},

	{
		module:      labelPresent,
		workerPod:   labelDifferent,
		schedulePod: labelMissing}: {getLabelName: utils.GetWorkerPodVersionLabelName, action: deleteAction},
}
