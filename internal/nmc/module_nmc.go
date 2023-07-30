package nmc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;watch;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs,verbs=get;list;watch;update;patch;create
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=nodemodulesconfigs/status,verbs=get;update;patch

type ModuleNMCHandler interface {
	handleModuleNMC(ctx context.Context, mld *api.ModuleLoaderData, nodes []v1.Node) error
}

type moduleNMCHandler struct {
	reconHelper moduleNMCHandlerHelperAPI
}

func NewModuleNMCHandler(client client.Client,
	nmcHelper Helper) ModuleNMCHandler {
	reconHelper := newModuleNMCHandlerHelper(client, nmcHelper)
	return &moduleNMCHandler{
		reconHelper: reconHelper,
	}
}

func (mnh *moduleNMCHandler) handleModuleNMC(ctx context.Context, mld *api.ModuleLoaderData, nodes []v1.Node) error {
	errs := make([]error, 0, len(nodes))
	for _, node := range nodes {
		nodeKernelVersion := strings.TrimSuffix(node.Status.NodeInfo.KernelVersion, "+")

		shouldBeOnNode, err := mnh.reconHelper.shouldModuleRunOnNode(node, mld, nodeKernelVersion)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if shouldBeOnNode {
			err = mnh.reconHelper.enableModuleOnNode(ctx, mld, node.Name, nodeKernelVersion)
		} else {
			err = mnh.reconHelper.disableModuleOnNode(ctx, mld.Namespace, mld.Name, node.Name)
		}

		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

//go:generate mockgen -source=module_nmc.go -package=nmc -destination=mock_module_nmc.go

type moduleNMCHandlerHelperAPI interface {
	shouldModuleRunOnNode(node v1.Node, mld *api.ModuleLoaderData, nodeKernelVersion string) (bool, error)
	enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, nodeName, kernelVersion string) error
	disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error
}

type moduleNMCHandlerHelper struct {
	client    client.Client
	nmcHelper Helper
}

func newModuleNMCHandlerHelper(client client.Client, nmcHelper Helper) moduleNMCHandlerHelperAPI {
	return &moduleNMCHandlerHelper{
		client:    client,
		nmcHelper: nmcHelper,
	}
}

func (mnhh *moduleNMCHandlerHelper) shouldModuleRunOnNode(node v1.Node, mld *api.ModuleLoaderData, nodeKernelVersion string) (bool, error) {
	if nodeKernelVersion != mld.KernelVersion {
		return false, nil
	}

	if !utils.IsNodeSchedulable(&node) {
		return false, nil
	}

	return utils.IsObjectSelectedByLabels(node.GetLabels(), mld.Selector)
}

func (mnhh *moduleNMCHandlerHelper) enableModuleOnNode(ctx context.Context, mld *api.ModuleLoaderData, nodeName, kernelVersion string) error {
	logger := log.FromContext(ctx)
	moduleConfig := kmmv1beta1.ModuleConfig{
		KernelVersion:        kernelVersion,
		ContainerImage:       mld.ContainerImage,
		InTreeModuleToRemove: mld.InTreeModuleToRemove,
		Modprobe:             mld.Modprobe,
	}

	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnhh.client, nmc, func() error {
		return mnhh.nmcHelper.SetModuleConfig(ctx, nmc, mld.Namespace, mld.Name, &moduleConfig)
	})

	if err != nil {
		return fmt.Errorf("failed to enable module %s/%s in NMC %s: %v", mld.Namespace, mld.Name, nodeName, err)
	}
	logger.Info("enable module in NMC", "name", mld.Name, "namespace", mld.Namespace, "node", nodeName, "result", opRes)
	return nil
}

func (mnhh *moduleNMCHandlerHelper) disableModuleOnNode(ctx context.Context, modNamespace, modName, nodeName string) error {
	logger := log.FromContext(ctx)
	nmc, err := mnhh.nmcHelper.Get(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get the NodeModulesConfig for node %s: %v", nodeName, err)
	}
	if nmc == nil {
		// NodeModulesConfig does not exists, module was never running on the node, we are good
		return nil
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, mnhh.client, nmc, func() error {
		return mnhh.nmcHelper.RemoveModuleConfig(ctx, nmc, modNamespace, modName)
	})

	if err != nil {
		return fmt.Errorf("failed to diable module %s/%s in NMC %s: %v", modNamespace, modName, nodeName, err)
	}

	logger.Info("disable module in NMC", "name", modName, "namespace", modNamespace, "node", nodeName, "result", opRes)
	return nil
}
