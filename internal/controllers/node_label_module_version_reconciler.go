package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type modulesVersionLabels struct {
	name                    string
	namespace               string
	moduleVersionLabel      string
	workerPodVersionLabel   string
	schedulePodVersionLabel string
}

const (
	NodeLabelModuleVersionReconcilerName = "NodeLabelModuleVersion"
)

type reconcileLabelsResult struct {
	labelsToAdd    map[string]string
	labelsToDelete []string
	requeue        bool
}

type NodeLabelModuleVersionReconciler struct {
	client    client.Client
	helperAPI nodeLabelModuleVersionHelperAPI
}

func NewNodeLabelModuleVersionReconciler(client client.Client) *NodeLabelModuleVersionReconciler {
	return &NodeLabelModuleVersionReconciler{
		client:    client,
		helperAPI: newNodeLabelModuleVersionHelper(client),
	}
}

func (nlmvr *NodeLabelModuleVersionReconciler) Reconcile(ctx context.Context, node *v1.Node) (ctrl.Result, error) {
	modulesVersionLabels := nlmvr.helperAPI.getLabelsPerModules(ctx, node.Labels)

	schedulePods, err := nlmvr.helperAPI.getSchedulePods(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get schedule pods for the node %s: %v", node.Name, err)
	}

	loadedKernelModules := nlmvr.helperAPI.getLoadedKernelModules(node.GetLabels())

	reconLabelsRes := nlmvr.helperAPI.reconcileLabels(modulesVersionLabels, schedulePods, loadedKernelModules)

	logger := log.FromContext(ctx).WithValues("node name", node.Name)
	logLabelsUpdateData(logger, reconLabelsRes)

	err = nlmvr.helperAPI.updateNodeLabels(ctx, node.Name, reconLabelsRes)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not label node %s with added/deleted labels: %v", node.Name, err)
	}

	return ctrl.Result{Requeue: reconLabelsRes.requeue}, nil
}

//go:generate mockgen -source=node_label_module_version_reconciler.go -package=controllers -destination=mock_node_label_module_version_reconciler.go nodeLabelModuleVersionHelperAPI

type nodeLabelModuleVersionHelperAPI interface {
	getLabelsPerModules(ctx context.Context, nodeLabels map[string]string) map[string]*modulesVersionLabels
	getSchedulePods(ctx context.Context, nodeName string) ([]v1.Pod, error)
	getLoadedKernelModules(labels map[string]string) []types.NamespacedName
	reconcileLabels(modulesLabels map[string]*modulesVersionLabels, schedulePods []v1.Pod, kernelModuleReadyLabels []types.NamespacedName) *reconcileLabelsResult
	updateNodeLabels(ctx context.Context, nodeName string, reconLabelsRes *reconcileLabelsResult) error
}

type nodeLabelModuleVersionHelper struct {
	client client.Client
}

func newNodeLabelModuleVersionHelper(client client.Client) nodeLabelModuleVersionHelperAPI {
	return &nodeLabelModuleVersionHelper{client: client}
}

func (nlmvha *nodeLabelModuleVersionHelper) getLabelsPerModules(ctx context.Context, nodeLabels map[string]string) map[string]*modulesVersionLabels {
	labelsPerModule := map[string]*modulesVersionLabels{}
	logger := log.FromContext(ctx)
	for key, value := range nodeLabels {
		if utils.IsVersionLabel(key) {
			namespace, name, err := utils.GetNamespaceNameFromVersionLabel(key)
			if err != nil {
				logger.Info(
					utils.WarnString("failed to extract namespace and name from version label"), "label", key, "labelValue", value,
				)
				continue
			}
			mapKey := namespace + "-" + name
			if labelsPerModule[mapKey] == nil {
				labelsPerModule[mapKey] = &modulesVersionLabels{name: name, namespace: namespace}
			}
			switch {
			case utils.IsModuleVersionLabel(key):
				labelsPerModule[mapKey].moduleVersionLabel = value
			case utils.IsWorkerPodVersionLabel(key):
				labelsPerModule[mapKey].workerPodVersionLabel = value
			case utils.IsSchedulePodVersionLabel(key):
				labelsPerModule[mapKey].schedulePodVersionLabel = value
			}
		}
	}

	return labelsPerModule
}

func (nlmvha *nodeLabelModuleVersionHelper) getSchedulePods(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	req, err := labels.NewRequirement(
		constants.DaemonSetRole,
		selection.In,
		[]string{constants.DevicePluginRoleLabelValue, constants.DRARoleLabelValue},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create label requirement: %v", err)
	}

	var podsList v1.PodList
	err = nlmvha.client.List(ctx, &podsList,
		client.HasLabels{constants.ModuleNameLabel},
		client.MatchingFields{"spec.nodeName": nodeName},
		client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*req)},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of schedule pods on node %s: %v", nodeName, err)
	}
	return podsList.Items, nil
}

func (nlmvha *nodeLabelModuleVersionHelper) getLoadedKernelModules(nodeLabels map[string]string) []types.NamespacedName {
	loadedKernelModules := make([]types.NamespacedName, 0, len(nodeLabels))
	for label := range nodeLabels {
		isReadyLabel, namespace, name := utils.IsKernelModuleReadyNodeLabel(label)
		if isReadyLabel {
			loadedKernelModules = append(loadedKernelModules, types.NamespacedName{Namespace: namespace, Name: name})
		}
	}
	return loadedKernelModules
}

func (nlmvha *nodeLabelModuleVersionHelper) updateNodeLabels(ctx context.Context, nodeName string, reconLabelsRes *reconcileLabelsResult) error {
	node := v1.Node{}

	if err := nlmvha.client.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return fmt.Errorf("could not get node %s: %v", nodeName, err)
	}

	nodeCopy := node.DeepCopy()

	if node.Labels == nil && len(reconLabelsRes.labelsToAdd) > 0 {
		node.Labels = make(map[string]string)
	}

	for label, labelValue := range reconLabelsRes.labelsToAdd {
		node.Labels[label] = labelValue
	}
	for _, label := range reconLabelsRes.labelsToDelete {
		delete(node.Labels, label)
	}

	return nlmvha.client.Patch(ctx, &node, client.MergeFrom(nodeCopy))
}

func (nlmvha *nodeLabelModuleVersionHelper) reconcileLabels(modulesLabels map[string]*modulesVersionLabels,
	schedulePods []v1.Pod,
	loadedKernelModules []types.NamespacedName) *reconcileLabelsResult {

	reconRes := reconcileLabelsResult{
		labelsToAdd:    map[string]string{},
		labelsToDelete: []string{},
	}
	for _, moduleLabels := range modulesLabels {
		label, labelValue, action := getLabelAndAction(moduleLabels)
		switch action {
		case deleteAction:
			if utils.IsWorkerPodVersionLabel(label) && !verifyLabelDeleteValidity(moduleLabels.name, moduleLabels.namespace, schedulePods) {
				reconRes.requeue = true
			} else {
				reconRes.labelsToDelete = append(reconRes.labelsToDelete, label)
			}
		case addAction:
			if utils.IsWorkerPodVersionLabel(label) && !verifyLabelAddValidity(moduleLabels.name, moduleLabels.namespace, loadedKernelModules) {
				reconRes.requeue = true
			} else {
				reconRes.labelsToAdd[label] = labelValue
			}
		}
	}

	return &reconRes
}

// verifyLabelDeleteValidity checks that no schedule pod (device plugin
// or DRA) matching the module name and namespace is present. If a matching pod
// exists, the delete action is invalid regardless of pod state.
func verifyLabelDeleteValidity(name, namespace string, pods []v1.Pod) bool {
	for _, pod := range pods {
		podLabels := pod.GetLabels()
		if pod.Namespace == namespace && podLabels[constants.ModuleNameLabel] == name {
			return false
		}
	}
	return true
}

func verifyLabelAddValidity(name, namespace string, loadedKernelModules []types.NamespacedName) bool {
	nsn := types.NamespacedName{Name: name, Namespace: namespace}
	for _, kernelModule := range loadedKernelModules {
		if kernelModule == nsn {
			return false
		}
	}
	return true
}

func getLabelAndAction(moduleLabels *modulesVersionLabels) (string, string, string) {
	labelActionKey := getModuleVersionLabelsState(moduleLabels)
	labelAction, ok := labelActionTable[labelActionKey]
	if !ok {
		return "", "", noneAction
	}
	switch labelAction.action {
	case noneAction:
		return "", "", noneAction
	case deleteAction:
		return labelAction.getLabelName(moduleLabels.namespace, moduleLabels.name), "", deleteAction
	default:
		return labelAction.getLabelName(moduleLabels.namespace, moduleLabels.name), moduleLabels.moduleVersionLabel, addAction
	}
}

func getModuleVersionLabelsState(moduleLabels *modulesVersionLabels) labelActionKey {
	key := labelActionKey{
		module:      labelPresent,
		workerPod:   labelPresent,
		schedulePod: labelPresent,
	}
	if moduleLabels.moduleVersionLabel == "" {
		key.module = labelMissing
	}
	if moduleLabels.workerPodVersionLabel == "" {
		key.workerPod = labelMissing
	} else if moduleLabels.moduleVersionLabel != "" && moduleLabels.workerPodVersionLabel != moduleLabels.moduleVersionLabel {
		key.workerPod = labelDifferent
	}
	if moduleLabels.schedulePodVersionLabel == "" {
		key.schedulePod = labelMissing
	} else if moduleLabels.moduleVersionLabel != "" && moduleLabels.schedulePodVersionLabel != moduleLabels.moduleVersionLabel {
		key.schedulePod = labelDifferent
	}
	return key
}

func logLabelsUpdateData(logger logr.Logger, reconLabelsRes *reconcileLabelsResult) {
	if len(reconLabelsRes.labelsToAdd) == 0 && len(reconLabelsRes.labelsToDelete) == 0 {
		logger.Info("no labels found for addtion/deletion", "requeue", reconLabelsRes.requeue)
	}
	if len(reconLabelsRes.labelsToAdd) != 0 {
		logger.Info("found labels to add", "addLabels", reconLabelsRes.labelsToAdd)
	}
	if len(reconLabelsRes.labelsToDelete) != 0 {
		logger.Info("found labels to delete", "deleteLabels", reconLabelsRes.labelsToDelete)
	}
}

func (nlmvr *NodeLabelModuleVersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.
		NewControllerManagedBy(mgr).
		Named(NodeLabelModuleVersionReconcilerName).
		For(&v1.Node{}).
		WithEventFilter(
			filter.NodeLabelModuleVersionUpdatePredicate(mgr.GetLogger().WithName("node-version-labeling-changed")),
		).
		Complete(
			reconcile.AsReconciler[*v1.Node](nlmvr.client, nlmvr),
		)
}
