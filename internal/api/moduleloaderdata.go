package api

import (
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils/image"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModuleLoaderData contains all the data needed for succesfull execution of
// Build,Sign, ModuleLoader flow per specific kernel. It is constructed at the begining
// of the Reconciliation loop flow.It contains all the data after merging/resolving all the
// definitions (Build/Sign configuration from KM or from Container, ContainerImage etc').
// From that point on , it is the only structure that is needed as input, no need for
// Module or for KernelMapping
type ModuleLoaderData struct {
	// kernel version
	KernelVersion string
	// Repo secret for DS images
	ImageRepoSecret *v1.LocalObjectReference

	// Selector for DS
	Selector map[string]string

	// Name
	Name string

	// Namespace
	Namespace string

	// service account for DS
	ServiceAccountName string

	// Build contains build instructions.
	Build *kmmv1beta1.Build

	// Sign provides default kmod signing settings
	Sign *kmmv1beta1.Sign

	// Module version
	ModuleVersion string

	// ContainerImage is a top-level field
	ContainerImage string

	// Image pull policy.
	ImagePullPolicy v1.PullPolicy

	// Modprobe is a set of properties to customize which module modprobe loads and with which properties.
	Modprobe kmmv1beta1.ModprobeSpec

	// RegistryTLS set the TLS configs for accessing the registry of the module-loader's image.
	RegistryTLS *kmmv1beta1.TLSOptions

	// used for setting the owner field of jobs/buildconfigs
	Owner metav1.Object
}

func (mld *ModuleLoaderData) BuildConfigured() bool {
	return mld.Build != nil
}

func (mld *ModuleLoaderData) BuildDestinationImage() (string, error) {
	if !mld.BuildConfigured() {
		return "", errors.New("no build configured")
	}

	if mld.Sign != nil {
		return mld.IntermediateImageName()
	}

	return mld.ContainerImage, nil
}

func (mld *ModuleLoaderData) IntermediateImageName() (string, error) {
	return image.SetOrAppendTag(
		mld.ContainerImage,
		fmt.Sprintf("%s_%s_kmm_unsigned", mld.Namespace, mld.Name),
		"_",
	)
}

func (mld *ModuleLoaderData) SignConfigured() bool {
	return mld.Sign != nil
}

func (mld *ModuleLoaderData) UnsignedImage() (string, error) {
	if !mld.SignConfigured() {
		return "", errors.New("signing not configured")
	}

	if build := mld.Build; build != nil {
		return mld.IntermediateImageName()
	}

	return mld.Sign.UnsignedImage, nil
}
