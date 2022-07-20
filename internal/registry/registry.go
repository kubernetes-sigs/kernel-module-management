package registry

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/auth"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	driverToolkitJSONFile = "etc/driver-toolkit-release.json"
)

type DriverToolkitEntry struct {
	ImageURL            string `json:"imageURL"`
	KernelFullVersion   string `json:"kernelFullVersion"`
	RTKernelFullVersion string `json:"RTKernelFullVersion"`
	OSVersion           string `json:"OSVersion"`
}

type RepoPullConfig struct {
	repo        string
	authOptions []crane.Option
}

//go:generate mockgen -source=registry.go -package=registry -destination=mock_registry_api.go

type Registry interface {
	ImageExists(ctx context.Context, image string, po ootov1alpha1.PullOptions, registryAuthGetter auth.RegistryAuthGetter) (bool, error)
	ExtractToolkitRelease(v1.Layer) (*DriverToolkitEntry, error)
	GetLayersDigests(ctx context.Context, image string, registryAuthGetter auth.RegistryAuthGetter) ([]string, *RepoPullConfig, error)
	GetLayerByDigest(digest string, pullConfig *RepoPullConfig) (v1.Layer, error)
}

type registry struct{}

func NewRegistry() Registry {
	return &registry{}
}

func (r *registry) ImageExists(ctx context.Context, image string, po ootov1alpha1.PullOptions, registryAuthGetter auth.RegistryAuthGetter) (bool, error) {
	pullConfig, err := r.getPullOptions(ctx, image, &po, registryAuthGetter)
	if err != nil {
		return false, fmt.Errorf("failed to get pull options for image %s: %w", image, err)
	}
	_, err = r.getImageManifest(ctx, image, pullConfig)
	if err != nil {
		te := &transport.Error{}
		if errors.As(err, &te) && te.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("could not get image %s: %w", image, err)
	}
	return true, nil
}

func (r *registry) GetLayersDigests(ctx context.Context, image string, registryAuthGetter auth.RegistryAuthGetter) ([]string, *RepoPullConfig, error) {
	pullConfig, err := r.getPullOptions(ctx, image, nil, registryAuthGetter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get pull options for image %s: %w", image, err)
	}
	manifest, err := r.getImageManifest(ctx, image, pullConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get manifest from image %s: %w", image, err)
	}

	digests, err := r.getLayersDigestsFromManifestStream(manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get layers digests from manifest of the image %s: %w", image, err)
	}

	return digests, pullConfig, nil
}

func (r *registry) GetLayerByDigest(digest string, pullConfig *RepoPullConfig) (v1.Layer, error) {
	return crane.PullLayer(pullConfig.repo+"@"+digest, pullConfig.authOptions...)
}

func (r *registry) ExtractToolkitRelease(layer v1.Layer) (*DriverToolkitEntry, error) {
	var found bool
	dtk := &DriverToolkitEntry{}
	obj, err := r.getHeaderFromLayer(layer, driverToolkitJSONFile)
	if err != nil {
		return nil, fmt.Errorf("failed to find file %s in image layer: %w", driverToolkitJSONFile, err)
	}

	dtk.KernelFullVersion, found, err = unstructured.NestedString(obj.Object, "KERNEL_VERSION")
	if !found || err != nil {
		return nil, fmt.Errorf("failed to get KERNEL_VERSION from %s, found %t: %w", driverToolkitJSONFile, found, err)
	}

	dtk.RTKernelFullVersion, found, err = unstructured.NestedString(obj.Object, "RT_KERNEL_VERSION")
	if !found || err != nil {
		return nil, fmt.Errorf("failed to get RT_KERNEL_VERSION from %s, found %t: %w", driverToolkitJSONFile, found, err)
	}

	dtk.OSVersion, found, err = unstructured.NestedString(obj.Object, "RHEL_VERSION")
	if !found || err != nil {
		return nil, fmt.Errorf("failed to get RHEL_VERSION from %s, found %t: %w", driverToolkitJSONFile, found, err)
	}
	return dtk, nil
}

func (r *registry) getPullOptions(ctx context.Context, image string, po *ootov1alpha1.PullOptions, registryAuthGetter auth.RegistryAuthGetter) (*RepoPullConfig, error) {
	var repo string
	if hash := strings.Split(image, "@"); len(hash) > 1 {
		repo = hash[0]
	} else if tag := strings.Split(image, ":"); len(tag) > 1 {
		repo = tag[0]
	}

	if repo == "" {
		return nil, fmt.Errorf("image url %s is not valid, does not contain hash or tag", image)
	}

	options := []crane.Option{
		crane.WithContext(ctx),
	}

	if po != nil {
		if po.Insecure {
			options = append(options, crane.Insecure)
		}

		if po.InsecureSkipTLSVerify {
			rt := http.DefaultTransport.(*http.Transport).Clone()
			rt.TLSClientConfig.InsecureSkipVerify = true

			options = append(
				options,
				crane.WithTransport(rt),
			)
		}
	}

	if registryAuthGetter != nil {
		keyChain, err := registryAuthGetter.GetKeyChain(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot get keychain from the registry auth getter: %w", err)
		}
		options = append(
			options,
			crane.WithAuthFromKeychain(keyChain),
		)
	}

	return &RepoPullConfig{repo: repo, authOptions: options}, nil
}

func (r *registry) getImageManifest(ctx context.Context, image string, pullConfig *RepoPullConfig) ([]byte, error) {
	manifest, err := r.getManifestStreamFromImage(image, pullConfig.repo, pullConfig.authOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest stream from image %s: %w", image, err)
	}

	return manifest, nil
}

func (r *registry) getManifestStreamFromImage(image, repo string, options []crane.Option) ([]byte, error) {
	manifest, err := crane.Manifest(image, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to get crane manifest from image %s: %w", image, err)
	}

	release := unstructured.Unstructured{}
	if err = json.Unmarshal(manifest, &release.Object); err != nil {
		return nil, fmt.Errorf("failed to unmarshal crane manifest: %w", err)
	}

	imageMediaType, mediaTypeFound, err := unstructured.NestedString(release.Object, "mediaType")
	if err != nil {
		return nil, fmt.Errorf("unmarshalled manifests invalid format: %w", err)
	}
	if !mediaTypeFound {
		return nil, fmt.Errorf("mediaType is missing from the image %s manifest", image)
	}

	if strings.Contains(imageMediaType, "manifest.list") {
		archDigest, err := r.getImageDigestFromMultiImage(manifest)
		if err != nil {
			return nil, fmt.Errorf("failed to get arch digets from multi arch image: %w", err)
		}
		// get the manifest stream for the image of the architecture
		manifest, err = crane.Manifest(repo+"@"+archDigest, options...)
		if err != nil {
			return nil, fmt.Errorf("failed to get crane manifest for the arch image: %w", err)
		}
	}
	return manifest, nil
}

func (r *registry) getLayersDigestsFromManifestStream(manifestStream []byte) ([]string, error) {
	manifest := v1.Manifest{}

	if err := json.Unmarshal(manifestStream, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest stream: %w", err)
	}

	digests := make([]string, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		digests[i] = layer.Digest.Algorithm + ":" + layer.Digest.Hex
	}
	return digests, nil
}

func (r *registry) getHeaderFromLayer(layer v1.Layer, headerName string) (*unstructured.Unstructured, error) {

	targz, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("failed to get targz from layer: %w", err)
	}
	// err ignored because we're only reading
	defer targz.Close()

	gr, err := gzip.NewReader(targz)
	if err != nil {
		return nil, fmt.Errorf("failed to create reader from targz: %w", err)
	}
	// err ignored because we're only reading
	defer gr.Close()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to get next entry from targz: %w", err)
		}
		if header.Name == headerName {
			buff, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read tar entry: %w", err)
			}

			obj := unstructured.Unstructured{}

			if err = json.Unmarshal(buff, &obj.Object); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tar entry: %w", err)
			}
			return &obj, nil
		}
	}

	return nil, fmt.Errorf("header %s not found in the layer", headerName)
}

func (r *registry) getImageDigestFromMultiImage(manifestListStream []byte) (string, error) {
	arch := runtime.GOARCH
	manifestList := v1.IndexManifest{}

	if err := json.Unmarshal(manifestListStream, &manifestList); err != nil {
		return "", fmt.Errorf("failed to unmarshal manifest stream: %w", err)
	}
	for _, manifest := range manifestList.Manifests {
		if manifest.Platform != nil && manifest.Platform.Architecture == arch {
			return manifest.Digest.Algorithm + ":" + manifest.Digest.Hex, nil
		}
	}
	return "", fmt.Errorf("Failed to find manifest for architecture %s", arch)
}
