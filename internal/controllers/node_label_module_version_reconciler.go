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
	moduleLoaderVersionLabel string
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

	modulesPods, err := nlmvr.helperAPI.getModuleLoaderAndDevicePluginPods(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could get module loader pods for the node %s: %v", node.Name, err)
	}

	reconLabelsRes := nlmvr.helperAPI.reconcileLabels(modulesVersionLabels, modulesPods)

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
	getModuleLoaderAndDevicePluginPods(ctx context.Context, nodeName string) ([]v1.Pod, error)
	reconcileLabels(modulesLabels map[string]*modulesVersionLabels, moduleLoaderPods []v1.Pod) *reconcileLabelsResult
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
			case utils.IsModuleLoaderVersionLabel(key):
				labelsPerModule[mapKey].moduleLoaderVersionLabel = value
			case utils.IsDevicePluginVersionLabel(key):
				labelsPerModule[mapKey].devicePluginVersionLabel = value
			}
		}
	}

	return labelsPerModule
}

func (nlmvha *nodeLabelModuleVersionHelper) getModuleLoaderAndDevicePluginPods(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	var modulePodsList v1.PodList
	fieldSelector := client.MatchingFields{"spec.nodeName": nodeName}
	labelSelector := client.HasLabels{constants.DaemonSetRole}
	err := nlmvha.client.List(ctx, &modulePodsList, labelSelector, fieldSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of all module loader pods for on node %s: %v", nodeName, err)
	}
	return modulePodsList.Items, nil
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
	modulesPods []v1.Pod) *reconcileLabelsResult {

	reconRes := reconcileLabelsResult{
		labelsToAdd:    map[string]string{},
		labelsToDelete: []string{},
	}
	for _, moduleLabels := range modulesLabels {
		label, labelValue, action := getLabelAndAction(moduleLabels)
		switch action {
		case deleteAction:
			validAction := verifyLabelActionValidity(moduleLabels.name, moduleLabels.namespace, constants.DevicePluginRoleLabelValue, modulesPods)
			if validAction {
				reconRes.labelsToDelete = append(reconRes.labelsToDelete, label)
			} else {
				reconRes.requeue = true
			}
		case addAction:
			validAction := verifyLabelActionValidity(moduleLabels.name, moduleLabels.namespace, constants.ModuleLoaderRoleLabelValue, modulesPods)
			if validAction {
				reconRes.labelsToAdd[label] = labelValue
			} else {
				reconRes.requeue = true
			}
		}
	}

	return &reconRes
}

// validity is checked by verifying that moduleLoader/devicePlugin pod defined
// by name, namespace,role is missing. In case the pod is present, no matter in
// what state, then the action is invalid
func verifyLabelActionValidity(name, namespace, podRole string, modulesPods []v1.Pod) bool {
	for _, pod := range modulesPods {
		podLabels := pod.GetLabels()
		if pod.Namespace == namespace &&
			podLabels[constants.ModuleNameLabel] == name &&
			podLabels[constants.DaemonSetRole] == podRole {
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
		moduleLoader: labelPresent,
		devicePlugin: labelPresent,
	}
	if moduleLabels.moduleVersionLabel == "" {
		key.module = labelMissing
	}
	if moduleLabels.moduleLoaderVersionLabel == "" {
		key.moduleLoader = labelMissing
	} else if moduleLabels.moduleVersionLabel != "" && moduleLabels.moduleLoaderVersionLabel != moduleLabels.moduleVersionLabel {
		key.moduleLoader = labelDifferent
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
