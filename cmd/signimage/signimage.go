package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"github.com/docker/cli/cli/config"
	dockertypes "github.com/docker/cli/cli/config/types"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"io"
	"k8s.io/klog/v2/klogr"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func checkArg(arg *string, varname string, fallback string) {
	if *arg == "" {
		if fallback != "" {
			*arg = fallback
		} else {
			fmt.Printf("%s not found:\n", varname)
			flag.PrintDefaults()
			os.Exit(0)
		}
	}
}

/*
** Convert a relative path to an absolute path
** filenames in image manifests are normally relative but can be "path/file" or "./path/file"
** or occasionaly be absolute "/path/file" (in baselayers) depending on how they were created.
** and we need to turn them into absolute paths for easy string comparisons against the
** filesToSign list we've been passed from the CR via the cli
** the easiest way to do this is force it to be abs, then clean it up
 */
func canonicalisePath(path string) string {
	return filepath.Clean("/" + path)
}

func signFile(filename string, publickey string, privatekey string) error {
	logger.Info("running /sign-file", "algo", "sha256", "privatekey", privatekey, "publickey", publickey, "filename", filepath.Base(filename))
	out, err := exec.Command("/sign-file", "sha256", privatekey, publickey, filename).Output()
	if err != nil {
		return fmt.Errorf("signing %s returned: %s\n error: %v\n", filename, out, err)
	}
	return nil
}

func getAuthFromFile(configfile string, repo string) (authn.Authenticator, error) {

	if configfile == "" {
		logger.Info("no pull secret defined, default to Anonymous")
		return authn.Anonymous, nil
	}

	f, err := os.Open(configfile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cf, err := config.LoadFromReader(f)
	if err != nil {
		return nil, err
	}

	var cfg dockertypes.AuthConfig

	cfg, err = cf.GetAuthConfig(repo)
	if err != nil {
		return nil, err
	}

	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil

}

func die(exitval int, message string, err error) {
	fmt.Fprintf(os.Stderr, "\n%s\n", message)
	logger.Info("ERROR "+message, "err", err)
	logger.Error(err, message)
	os.Exit(exitval)
}

func processFile(filename string, header *tar.Header, tarreader io.Reader, data []interface{}) error {

	registryObj := data[0].(registry.Registry)
	extractionDir := data[1].(string)
	filesList := data[2].(string)
	privKeyFile := data[3].(string)
	pubKeyFile := data[4].(string)
	kmodsToSign := data[5].(map[string]string)

	canonfilename := canonicalisePath(filename)

	//either the kmod has not yet been found, or we didn't define a list to search for
	if kmodsToSign[canonfilename] == "not found" ||
		(filesList == "" &&
			kmodsToSign[canonfilename] == "" &&
			canonfilename[len(canonfilename)-3:] == ".ko") {

		logger.Info("Found kmod", "kmod", canonfilename, "matches kmod in image", header.Name)
		//its a file we wanted and haven't already seen
		//extract to the local filesystem
		err := registryObj.ExtractFileToFile(extractionDir+"/"+header.Name, header, tarreader)
		if err != nil {
			return err
		}
		kmodsToSign[canonfilename] = extractionDir + "/" + header.Name
		logger.Info("Signing kmod", "kmod", canonfilename)

		//sign it
		err = signFile(kmodsToSign[canonfilename], pubKeyFile, privKeyFile)
		if err != nil {
			return fmt.Errorf("error signing file %s", canonfilename)
		}
		logger.Info("Signed successfully", "kmod", canonfilename)
		return nil

	}
	return nil
}

func addFileToTarball(sourcename string, filename string, tarwriter *tar.Writer) error {
	finfo, err := os.Stat(sourcename)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", filename, err)
	}

	if finfo.IsDir() {
		return nil
	}
	hdr := &tar.Header{
		Name:     filename,
		Mode:     int64(finfo.Mode()),
		Typeflag: 0,
		Size:     finfo.Size(),
	}

	if err := tarwriter.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	f, err := os.Open(sourcename)
	if err != nil {
		return fmt.Errorf("failed to open file to add to the tar: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(tarwriter, f); err != nil {
		return fmt.Errorf("failed to read file into the tar: %w", err)
	}

	return nil
}

var logger logr.Logger

func main() {
	// get the env vars we are using for setup, or set some sensible defaults
	var err error
	var unsignedImageName string
	var signedImageName string
	var pullSecret string
	var pushSecret string
	var extractionDir string
	var filesList string
	var privKeyFile string
	var pubKeyFile string
	var nopush bool

	logger = klogr.New()

	flag.StringVar(&unsignedImageName, "unsignedimage", "", "name of the image to sign")
	flag.StringVar(&signedImageName, "signedimage", "", "name of the signed image to produce")
	flag.StringVar(&filesList, "filestosign", "", "colon seperated list of kmods to sign")
	flag.StringVar(&privKeyFile, "key", "", "path to file containing private key for signing")
	flag.StringVar(&pubKeyFile, "cert", "", "path to file containing public key for signing")
	flag.StringVar(&pullSecret, "pullsecret", "", "path to file containing credentials for pulling images")
	flag.StringVar(&pullSecret, "pushsecret", "", "path to file containing credentials for pushing images")
	flag.BoolVar(&nopush, "no-push", false, "do not push the resulting image")

	flag.Parse()

	checkArg(&unsignedImageName, "unsignedimage", "")
	checkArg(&signedImageName, "signedimage", unsignedImageName+"signed")
	checkArg(&filesList, "filestosign", "")
	checkArg(&privKeyFile, "key", "")
	checkArg(&pubKeyFile, "cert", "")
	checkArg(&pullSecret, "pullsecret", "")
	checkArg(&pushSecret, "pushsecret", pullSecret)
	// if we've made it this far the arguments are sane

	// get a temp dir to copy kmods into for signing
	extractionDir, err = os.MkdirTemp("/tmp/", "kmod_signer")
	if err != nil {
		die(1, "could not create temp dir", err)
	}
	defer os.RemoveAll(extractionDir)

	// sets up a tar archive we will use for a new layer
	var b bytes.Buffer
	tarwriter := tar.NewWriter(&b)

	//make a map of the files to sign so we can track what we want to sign
	kmodsToSign := make(map[string]string)
	for _, x := range strings.Split(filesList, ":") {
		if canonicalisePath(x) != x {
			err = fmt.Errorf("%s not an sbsolute path", x)
			die(9, "paths for files to sign must be absolute", err)
		}
		kmodsToSign[x] = "not found"
	}

	a, err := getAuthFromFile(pullSecret, strings.Split(unsignedImageName, "/")[0])
	if err != nil {
		die(2, "failed to get auth", err)
	}

	r := registry.NewRegistry()

	img, err := r.GetImageByName(unsignedImageName, a)
	if err != nil {
		die(3, "could not Image()", err)
	}

	logger.Info("Successfully pulled image", "image", unsignedImageName)
	logger.Info("Looking for files", "filelist", strings.Replace(filesList, ":", " ", -1))

	/*
	** loop through all the layers in the image from the top down
	 */
	err = r.WalkFilesInImage(img, processFile, r, extractionDir, filesList, privKeyFile, pubKeyFile, kmodsToSign)
	if err != nil {
		die(9, "failed to search image", err)
	}

	/*
	** check if we found everything, if not then explode
	 */
	missingKmods := 0
	for k, v := range kmodsToSign {
		if v == "not found" {
			missingKmods = 1
			logger.Info("Failed to find expected kmod", "kmod", k)
		} else {
			err := addFileToTarball(v, k, tarwriter)
			if err != nil {
				die(1, "failed to add signed kmods to tarball", nil)
			}

		}
	}
	if missingKmods != 0 {
		die(4, "Failed to find all expected kmods", fmt.Errorf("Failed to find all expected kmods"))
	}

	outputTarFile := extractionDir + "/layerfile.tar"
	err = os.WriteFile(outputTarFile, b.Bytes(), 0700)
	if err != nil {
		die(5, "failed to write layer to tarball", err)
	}

	//create a new image from our old image with our tarball as a new layer
	signedImage, err := r.AddLayerToImage(outputTarFile, img)
	if err != nil {
		die(6, "failed to add layer to image", err)
	}

	logger.Info("Appended new layer to image", "image", signedImageName)

	if !nopush {
		a, err = getAuthFromFile(pushSecret, strings.Split(signedImageName, "/")[0])
		if err != nil {
			die(7, "failed to get push auth", err)
		}

		// write the image back to the name:tag set via the args
		err := r.WriteImageByName(signedImageName, signedImage, a)
		if err != nil {
			die(8, "failed to write signed image", err)
		}
		// we're done successfully, so we need a nice friendly message to say that
		logger.Info("Pushed image back to repo", "image", signedImageName)
	}
	os.Exit(0)
}
