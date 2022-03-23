package controllers

import (
	"errors"
	"fmt"

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:generate mockgen -source=daemonset.go -package=controllers -destination=mock_daemonset.go

type DaemonSetCreator interface {
	SetAsDesired(ds *appsv1.DaemonSet, image string, mod *ootov1beta1.Module, kernelVersion string) error
}

type daemonSetGenerator struct {
	kernelLabel string
	scheme      *runtime.Scheme
}

func NewDaemonSetCreator(kernelLabel string, scheme *runtime.Scheme) *daemonSetGenerator {
	return &daemonSetGenerator{
		kernelLabel: kernelLabel,
		scheme:      scheme,
	}
}

func (dc *daemonSetGenerator) SetAsDesired(ds *appsv1.DaemonSet, image string, mod *ootov1beta1.Module, kernelVersion string) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if image == "" {
		return errors.New("image cannot be empty")
	}

	if mod == nil {
		return errors.New("mod cannot be nil")
	}

	if kernelVersion == "" {
		return errors.New("kernelVersion cannot be empty")
	}

	standardLabels := map[string]string{
		"oot.node.kubernetes.io/module.name":         mod.Name,
		"oot.node.kubernetes.io/kernel-version.full": kernelVersion,
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

	driverContainer := mod.Spec.DriverContainer
	driverContainer.Name = "driver-container"
	driverContainer.Image = image

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: standardLabels},
			Spec: v1.PodSpec{
				NodeSelector: nodeSelector,
				Containers:   []v1.Container{driverContainer},
			},
		},
	}

	if err := controllerutil.SetOwnerReference(mod, ds, dc.scheme); err != nil {
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
