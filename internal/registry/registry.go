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
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/auth"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	modulesLocationPath = "lib/modules"
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
	ImageExists(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) (bool, error)
	VerifyModuleExists(layer v1.Layer, pathPrefix, kernelVersion, moduleFileName string) bool
	GetLayersDigests(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) ([]string, *RepoPullConfig, error)
	GetLayerByDigest(digest string, pullConfig *RepoPullConfig) (v1.Layer, error)
	WriteImageByName(imageName string, image v1.Image, auth authn.Authenticator) error
	WalkFilesInImage(image v1.Image, fn func(filename string, header *tar.Header, tarreader io.Reader, data []interface{}) error, data ...interface{}) error
	GetLayerMediaType(image v1.Image) (types.MediaType, error)
	AddLayerToImage(tarfile string, image v1.Image) (v1.Image, error)
	GetImageByName(imageName string, auth authn.Authenticator) (v1.Image, error)
	ParseReference(imageName string) (name.Reference, error)
	ExtractBytesFromTar(size int64, tarreader io.Reader) ([]byte, error)
	ExtractFileToFile(destination string, header *tar.Header, tarreader io.Reader) error
}

type registry struct {
}

func NewRegistry() Registry {
	return &registry{}
}

func (r *registry) ImageExists(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) (bool, error) {
	_, _, err := r.getImageManifest(ctx, image, tlsOptions, registryAuthGetter)
	if err != nil {
		te := &transport.Error{}
		if errors.As(err, &te) && te.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, fmt.Errorf("could not get image %s: %w", image, err)
	}
	return true, nil
}

func (r *registry) GetLayersDigests(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) ([]string, *RepoPullConfig, error) {
	manifest, pullConfig, err := r.getImageManifest(ctx, image, tlsOptions, registryAuthGetter)
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

func (r *registry) VerifyModuleExists(layer v1.Layer, pathPrefix, kernelVersion, moduleFileName string) bool {
	// in layers headers, there is no root prefix
	fullPath := filepath.Join(strings.TrimPrefix(pathPrefix, "/"), modulesLocationPath, kernelVersion, moduleFileName)
	_, err := r.getHeaderStreamFromLayer(layer, fullPath)
	return err == nil
}

func (r *registry) getPullOptions(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) (*RepoPullConfig, error) {
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

	if tlsOptions != nil {
		if tlsOptions.Insecure {
			options = append(options, crane.Insecure)
		}

		if tlsOptions.InsecureSkipTLSVerify {
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

func (r *registry) getImageManifest(ctx context.Context, image string, tlsOptions *kmmv1beta1.TLSOptions, registryAuthGetter auth.RegistryAuthGetter) ([]byte, *RepoPullConfig, error) {
	pullConfig, err := r.getPullOptions(ctx, image, tlsOptions, registryAuthGetter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get pull options for image %s: %w", image, err)
	}
	manifest, err := r.getManifestStreamFromImage(image, pullConfig.repo, pullConfig.authOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get manifest stream from image %s: %w", image, err)
	}

	return manifest, pullConfig, nil
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

func (r *registry) getHeaderStreamFromLayer(layer v1.Layer, headerName string) (io.Reader, error) {

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
			return tr, nil
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

func (r *registry) GetImageByName(imageName string, auth authn.Authenticator) (v1.Image, error) {

	ref, err := r.ParseReference(imageName)
	if err != nil {
		return nil, err
	}

	descriptor, err := remote.Get(ref, remote.WithAuth(auth))
	if err != nil {
		return nil, fmt.Errorf("could not get image: %w", err)
	}

	img, err := descriptor.Image()
	if err != nil {
		return nil, fmt.Errorf("could not call image: %w", err)
	}
	return img, nil
}

func (r *registry) ParseReference(imageName string) (name.Reference, error) {
	opts := make([]name.Option, 0)
	ref, err := name.ParseReference(imageName, opts...)
	if err != nil {
		return nil, fmt.Errorf("could not parse the container image %s: %w", imageName, err)
	}

	return ref, nil
}

func (r *registry) WriteImageByName(imageName string, image v1.Image, auth authn.Authenticator) error {

	ref, err := r.ParseReference(imageName)
	if err != nil {
		return err
	}

	err = remote.Write(ref, image, remote.WithAuth(auth))
	if err != nil {
		return fmt.Errorf("failed to push signed image: %w", err)
	}
	return nil
}

func (r *registry) AddLayerToImage(tarfile string, image v1.Image) (v1.Image, error) {

	//turn our tar archive into a layer
	mt, err := r.GetLayerMediaType(image)
	if err != nil {
		return nil, err
	}

	signedLayer, err := tarball.LayerFromFile(tarfile, tarball.WithMediaType(mt))
	if err != nil {
		return nil, fmt.Errorf("failed to generate layer from tar: %w", err)
	}

	// add the layer to the unsigned image
	newImage, err := mutate.AppendLayers(image, signedLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to append layer: %w", err)
	}

	// this is needde because mutate.AppendLayers loses the image mediatype
	// without it the controller refuses to run the resulting images
	imageMediaType, _ := image.MediaType()
	newImageWithMT := mutate.MediaType(newImage, imageMediaType)
	if err != nil {
		return nil, fmt.Errorf("failed to change medaitype of image: %w", err)
	}
	return newImageWithMT, nil
}

func (r *registry) GetLayerMediaType(image v1.Image) (types.MediaType, error) {
	layers, err := image.Layers()
	if err != nil {
		return types.OCILayer, fmt.Errorf("could not get the layers from image: %w", err)
	}
	return layers[len(layers)-1].MediaType()
}

/*
** generic function to loop through all the files in an image and run
** a function on them, based loosly on ftw() or filepath.Walk().
** image   - the image to examine
** fn	   - the function to call on each file
** ...data - all other arguements are passed through to the helper function
**	     to provide any additional data needed.
 */
func (r *registry) WalkFilesInImage(image v1.Image, fn func(filename string, header *tar.Header, tarreader io.Reader, data []interface{}) error, data ...interface{}) error {
	layers, err := image.Layers()
	if err != nil {
		return fmt.Errorf("could not get the layers from the fetched image: %w", err)
	}
	for i := len(layers) - 1; i >= 0; i-- {
		currentlayer := layers[i]

		layerreader, err := currentlayer.Uncompressed()
		if err != nil {
			return fmt.Errorf("could not get layer: %w", err)
		}

		/*
		** call fn on all the files in the layer
		 */
		tarreader := tar.NewReader(layerreader)
		for {
			header, err := tarreader.Next()
			if err == io.EOF || header == nil {
				break // End of archive
			}
			err = fn(header.Name, header, tarreader, data)
			if err != nil {
				return fmt.Errorf("died processing file %s: %w", header.Name, err)
			}
		}
	}

	return err
}

/*
** extract size bytres from the start of an io.Reader
 */
func (r *registry) ExtractBytesFromTar(size int64, tarreader io.Reader) ([]byte, error) {

	contents := make([]byte, size)
	offset := 0
	for {
		rc, err := tarreader.Read(contents[offset:])
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("could not read from tar: %w ", err)
		}
		offset += rc
		if err == io.EOF {
			break
		}
	}
	return contents, nil
}

/*
** extract the next file from a pre-positioned io.Reader to destination
 */
func (r *registry) ExtractFileToFile(destination string, header *tar.Header, tarreader io.Reader) error {

	contents, err := r.ExtractBytesFromTar(header.Size, tarreader)
	if err != nil {
		return fmt.Errorf("could not read file %s: %w", destination, err)
	}

	dirname := filepath.Dir(destination)

	// I hope you've set your umask to something sensible!
	err = os.MkdirAll(dirname, 0770)
	if err != nil {
		return fmt.Errorf("could not create directory structure for %s: %w", destination, err)
	}
	err = os.WriteFile(destination, contents, 0700)
	if err != nil {
		return fmt.Errorf("could not create temp %s: %w", destination, err)
	}
	return nil

}
