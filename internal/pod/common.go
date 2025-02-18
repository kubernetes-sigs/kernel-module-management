package pod

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deletePod(clnt client.Client, ctx context.Context, pod *v1.Pod) error {

	logger := ctrl.LoggerFrom(ctx)

	if pod.DeletionTimestamp != nil {
		logger.Info("DeletionTimestamp set, pod is already in deletion", "pod", pod.Name)
		return nil
	}

	if err := clnt.Delete(ctx, pod); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete pull pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	return nil
}
