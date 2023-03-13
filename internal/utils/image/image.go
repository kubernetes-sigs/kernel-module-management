package image

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

func setTag(imageName, tag string) string {
	return fmt.Sprintf("%s:%s", imageName, tag)
}

func SetTag(imageName, newTag string) (string, error) {
	ref, err := name.ParseReference(imageName, name.WithDefaultTag(""), name.WithDefaultRegistry(""))
	if err != nil {
		return "", fmt.Errorf("could not parse the image name: %v", err)
	}

	return setTag(ref.Context().Name(), newTag), nil
}

func SetOrAppendTag(imageName, newTag, sep string) (string, error) {
	ref, err := name.ParseReference(imageName, name.WithDefaultTag(""), name.WithDefaultRegistry(""))
	if err != nil {
		return "", fmt.Errorf("could not parse the image name: %v", err)
	}

	// Only digest names contain a semicolon
	if identifier := ref.Identifier(); identifier != "" && !strings.Contains(identifier, ":") {
		newTag = identifier + sep + newTag
	}

	return setTag(ref.Context().Name(), newTag), nil
}
