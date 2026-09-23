/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pluginCleanupRequeue paces a cleanup pass: nothing wakes the controller when the last Pod goes.
const pluginCleanupRequeue = 2 * time.Second

// deletePluginObjects asks the objects that are not already going to go, and reports how many it
// saw, since one that is still finalizing has not gone. Each delete pins what was read.
func deletePluginObjects(ctx context.Context, c client.Client, objs []client.Object) (int, error) {
	var errs []error

	for _, obj := range objs {
		// Asking again puts the GC finalizer back, and every one of those writes wakes us again.
		if obj.GetDeletionTimestamp() != nil {
			continue
		}

		opts := &client.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID:             ptr.To(obj.GetUID()),
				ResourceVersion: ptr.To(obj.GetResourceVersion()),
			},
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		}

		if err := c.Delete(ctx, obj, opts); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("could not delete %s %s/%s: %v",
				obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err))
		}
	}

	return len(objs), errors.Join(errs...)
}

// pluginPodsRemain reports whether a Pod a plugin DaemonSet owns is still an object, whatever its
// phase. The count a DaemonSet reports says what is wanted, not what is left.
func pluginPodsRemain(
	ctx context.Context,
	reader client.Reader,
	namespace, moduleName string,
	roleMatches func(string) bool,
) (bool, error) {
	pods := v1.PodList{}

	opts := []client.ListOption{
		client.MatchingLabels{constants.ModuleNameLabel: moduleName},
		client.InNamespace(namespace),
	}
	if err := reader.List(ctx, &pods, opts...); err != nil {
		return false, fmt.Errorf("could not list pods for module %s/%s: %v", namespace, moduleName, err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]

		// Worker pods carry the same module label but answer to the NMC, not to a plugin.
		owner := metav1.GetControllerOf(pod)
		if owner == nil || owner.Kind != "DaemonSet" {
			continue
		}

		if roleMatches(pod.GetLabels()[constants.DaemonSetRole]) {
			return true, nil
		}
	}

	return false, nil
}
