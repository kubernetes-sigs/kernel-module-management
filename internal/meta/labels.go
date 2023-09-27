package meta

import "sigs.k8s.io/controller-runtime/pkg/client"

func RemoveLabel(obj client.Object, key string) {
	labels := obj.GetLabels()

	if labels == nil {
		return
	}

	delete(labels, key)

	obj.SetLabels(labels)
}

func SetLabel(obj client.Object, key, value string) {
	labels := obj.GetLabels()

	if labels == nil {
		labels = make(map[string]string, 1)
	}

	labels[key] = value

	obj.SetLabels(labels)
}
