package meta

import "sigs.k8s.io/controller-runtime/pkg/client"

func SetAnnotation(obj client.Object, key, value string) {
	ann := obj.GetAnnotations()

	if ann == nil {
		ann = make(map[string]string, 1)
	}

	ann[key] = value

	obj.SetAnnotations(ann)
}
