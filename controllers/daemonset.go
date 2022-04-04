package controllers

import (
	"context"
	"errors"
	"fmt"

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:generate mockgen -source=daemonset.go -package=controllers -destination=mock_daemonset.go

const ModuleNameLabel = "oot.node.kubernetes.io/module.name"

type DaemonSetCreator interface {
	ModuleDaemonSetsByKernelVersion(ctx context.Context, mod ootov1beta1.Module) (map[string]*appsv1.DaemonSet, error)
	SetAsDesired(ds *appsv1.DaemonSet, image string, mod ootov1beta1.Module, kernelVersion string) error
}

type daemonSetGenerator struct {
	client      client.Client
	kernelLabel string
	namespace   string
	scheme      *runtime.Scheme
}

func NewDaemonSetCreator(client client.Client, kernelLabel, namespace string, scheme *runtime.Scheme) *daemonSetGenerator {
	return &daemonSetGenerator{
		client:      client,
		kernelLabel: kernelLabel,
		namespace:   namespace,
		scheme:      scheme,
	}
}

func (dc *daemonSetGenerator) ModuleDaemonSetsByKernelVersion(ctx context.Context, mod ootov1beta1.Module) (map[string]*appsv1.DaemonSet, error) {
	dsList := appsv1.DaemonSetList{}

	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{ModuleNameLabel: mod.Name}),
		client.InNamespace(dc.namespace),
	}

	if err := dc.client.List(ctx, &dsList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DaemonSets: %v", err)
	}

	dsByKernelVersion := make(map[string]*appsv1.DaemonSet, len(dsList.Items))

	for i := 0; i < len(dsList.Items); i++ {
		ds := dsList.Items[i]

		kernelVersion := ds.Labels[dc.kernelLabel]

		if dsByKernelVersion[kernelVersion] != nil {
			return nil, fmt.Errorf("multiple DaemonSets found for kernel %q", kernelVersion)
		}

		dsByKernelVersion[kernelVersion] = &ds
	}

	return dsByKernelVersion, nil
}

func (dc *daemonSetGenerator) SetAsDesired(ds *appsv1.DaemonSet, image string, mod ootov1beta1.Module, kernelVersion string) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if image == "" {
		return errors.New("image cannot be empty")
	}

	if kernelVersion == "" {
		return errors.New("kernelVersion cannot be empty")
	}

	standardLabels := map[string]string{
		ModuleNameLabel: mod.Name,
		dc.kernelLabel:  kernelVersion,
	}

	labels := ds.GetLabels()

	if labels == nil {
		labels = make(map[string]string, len(standardLabels))
	}

	for k, v := range standardLabels {
		labels[k] = v
	}

	ds.SetLabels(labels)

	nodeSelector := CopyMapStringString(mod.Spec.Selector)
	nodeSelector[dc.kernelLabel] = kernelVersion

	const (
		nodeLibModulesPath          = "/lib/modules"
		nodeLibModulesVolumeName    = "node-lib-modules"
		nodeUsrLibModulesPath       = "/usr/lib/modules"
		nodeUsrLibModulesVolumeName = "node-usr-lib-modules"
	)

	driverContainer := mod.Spec.DriverContainer
	driverContainer.Name = "driver-container"
	driverContainer.Image = image
	driverContainer.VolumeMounts = []v1.VolumeMount{
		{
			Name:      nodeLibModulesVolumeName,
			ReadOnly:  true,
			MountPath: nodeLibModulesPath,
		},
		{
			Name:      nodeUsrLibModulesVolumeName,
			ReadOnly:  true,
			MountPath: nodeUsrLibModulesPath,
		},
	}

	hostPathDirectory := v1.HostPathDirectory

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: standardLabels},
			Spec: v1.PodSpec{
				NodeSelector: nodeSelector,
				Containers:   []v1.Container{driverContainer},
				Volumes: []v1.Volume{
					{
						Name: nodeLibModulesVolumeName,
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: nodeLibModulesPath,
								Type: &hostPathDirectory,
							},
						},
					},
					{
						Name: nodeUsrLibModulesVolumeName,
						VolumeSource: v1.VolumeSource{
							HostPath: &v1.HostPathVolumeSource{
								Path: nodeUsrLibModulesPath,
								Type: &hostPathDirectory,
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetOwnerReference(&mod, ds, dc.scheme); err != nil {
		return fmt.Errorf("could not set the owner reference: %v", err)
	}

	return nil
}

// CopyMapStringString returns a deep copy of m.
func CopyMapStringString(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))

	for k, v := range m {
		n[k] = v
	}

	return n
}
