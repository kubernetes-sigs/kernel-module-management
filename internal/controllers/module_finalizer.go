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
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// setModuleFinalizer adds or removes one finalizer and reports whether it wrote anything. The patch
// carries the resourceVersion it read, so it fails rather than dropping one added in between.
func setModuleFinalizer(
	ctx context.Context,
	c client.Client,
	mod *kmmv1beta1.Module,
	finalizer string,
	present bool,
) (bool, error) {
	if controllerutil.ContainsFinalizer(mod, finalizer) == present {
		return false, nil
	}

	// The API server refuses a finalizer added after deletion has started.
	if present && mod.GetDeletionTimestamp() != nil {
		return false, fmt.Errorf("cannot add finalizer %s to deleting module %s/%s", finalizer, mod.Namespace, mod.Name)
	}

	before := mod.DeepCopy()
	updated := mod.DeepCopy()

	if present {
		controllerutil.AddFinalizer(updated, finalizer)
	} else {
		controllerutil.RemoveFinalizer(updated, finalizer)
	}

	patch := client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})
	if err := c.Patch(ctx, updated, patch); err != nil {
		// A module that is gone needs no release, but it cannot protect a write either.
		if !present && apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("could not patch finalizer %s on module %s/%s: %v", finalizer, mod.Namespace, mod.Name, err)
	}

	*mod = *updated

	return true, nil
}
