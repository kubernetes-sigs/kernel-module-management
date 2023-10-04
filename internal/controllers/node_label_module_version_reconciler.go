package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;watch;patch

// this struct contains all the version labels related to a specific Module
type modulesVersionLabels struct {
	name                     string
	namespace                string
	moduleVersionLabel       string
	workerPodVersionLabel    string
	devicePluginVersionLabel string
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

func (nlmvr *NodeLabelModuleVersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := v1.Node{}

	if err := nlmvr.client.Get(ctx, types.NamespacedName{Name: req.Name}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get node: %v", err)
	}

	modulesVersionLabels := nlmvr.helperAPI.getLabelsPerModules(ctx, node.Labels)

	devicePluginPods, err := nlmvr.helperAPI.getDevicePluginPods(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could get device plugin pods for the node %s: %v", node.Name, err)
	}

	loadedKernelModules := nlmvr.helperAPI.getLoadedKernelModules(node.GetLabels())

	reconLabelsRes := nlmvr.helperAPI.reconcileLabels(modulesVersionLabels, devicePluginPods, loadedKernelModules)

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
	getDevicePluginPods(ctx context.Context, nodeName string) ([]v1.Pod, error)
	getLoadedKernelModules(labels map[string]string) []types.NamespacedName
	reconcileLabels(modulesLabels map[string]*modulesVersionLabels, devicePluginPods []v1.Pod, kernelModuleReadyLabels []types.NamespacedName) *reconcileLabelsResult
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
				logger.Info(utils.WarnString("failed to extract namespace and name from version label"), "label", key, "labelValue", value)
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
			case utils.IsDevicePluginVersionLabel(key):
				labelsPerModule[mapKey].devicePluginVersionLabel = value
			}
		}
	}

	return labelsPerModule
}

func (nlmvha *nodeLabelModuleVersionHelper) getDevicePluginPods(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	var kmmPodsList v1.PodList
	fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
	labelSelector := client.HasLabels{constants.ModuleNameLabel}
	err := nlmvha.client.List(ctx, &kmmPodsList, labelSelector, fieldSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of all device plugin pods for on node %s: %v", nodeName, err)
	}

	devicePluginPods := make([]v1.Pod, 0, len(kmmPodsList.Items))
	for _, pod := range kmmPodsList.Items {
		for _, ownerReference := range pod.ObjectMeta.OwnerReferences {
			if ownerReference.Kind == "DaemonSet" {
				devicePluginPods = append(devicePluginPods, pod)
				break
			}
		}
	}
	return devicePluginPods, nil
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
	devicePluginPods []v1.Pod,
	loadedKernelModules []types.NamespacedName) *reconcileLabelsResult {

	reconRes := reconcileLabelsResult{
		labelsToAdd:    map[string]string{},
		labelsToDelete: []string{},
	}
	for _, moduleLabels := range modulesLabels {
		label, labelValue, action := getLabelAndAction(moduleLabels)
		switch action {
		case deleteAction:
			if utils.IsWorkerPodVersionLabel(label) && !verifyLabelDeleteValidity(moduleLabels.name, moduleLabels.namespace, devicePluginPods) {
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

// validity is checked by verifying that devicePlugin pod defined
// by name, namespace,role is missing. In case the pod is present, no matter in
// what state, then the action is invalid
func verifyLabelDeleteValidity(name, namespace string, devicePluginPods []v1.Pod) bool {
	for _, pod := range devicePluginPods {
		podLabels := pod.GetLabels()
		if pod.Namespace == namespace && podLabels[constants.ModuleNameLabel] == name {
			return false
		}
	}
	return true
}

// validity is checked by verifying that kernel module defined
// by name and namespace is not loaded. In case the kernel module is loaded, then the action is invalid
func verifyLabelAddValidity(name, namespace string, loadedKernelModules []types.NamespacedName) bool {
	nsn := types.NamespacedName{Name: name, Namespace: namespace}
	for _, kernelModule := range loadedKernelModules {
		if kernelModule == nsn {
			return false
		}
	}
	return true
}

// returns the label, value of the label and action to execute on label (add or delete)
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
		module:       labelPresent,
		workerPod:    labelPresent,
		devicePlugin: labelPresent,
	}
	if moduleLabels.moduleVersionLabel == "" {
		key.module = labelMissing
	}
	if moduleLabels.workerPodVersionLabel == "" {
		key.workerPod = labelMissing
	} else if moduleLabels.moduleVersionLabel != "" && moduleLabels.workerPodVersionLabel != moduleLabels.moduleVersionLabel {
		key.workerPod = labelDifferent
	}
	if moduleLabels.devicePluginVersionLabel == "" {
		key.devicePlugin = labelMissing
	} else if moduleLabels.moduleVersionLabel != "" && moduleLabels.devicePluginVersionLabel != moduleLabels.moduleVersionLabel {
		key.devicePlugin = labelDifferent
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
		For(&v1.Node{}).WithEventFilter(
		filter.NodeLabelModuleVersionUpdatePredicate(mgr.GetLogger().WithName("node-version-labeling-changed")),
	).
		Complete(nlmvr)
}
