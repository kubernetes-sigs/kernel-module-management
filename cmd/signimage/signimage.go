package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"io"
	"io/fs"
	"k8s.io/klog/v2/klogr"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// configDir should contain all the secrets available as individual files named for their keys
// so we need to search through for the secrets we need for our pull and push repos.
// If we do not find any authn, default to anonymous.
// we're (trying to be) tolerant in our inputs, precise in our outputs, and opaque in our comments. Its the UNIX way!
type repoAuth struct {
	pullRepo  string
	pushRepo  string
	PullAuth  authn.Authenticator
	PushAuth  authn.Authenticator
	configDir string
}

func NewRepoAuth(configDir string, pullRepo string, pushRepo string) *repoAuth {
	if pushRepo == "" {
		pushRepo = pullRepo
	}
	r := &repoAuth{
		pullRepo:  pullRepo,
		pushRepo:  pushRepo,
		PullAuth:  nil,
		PushAuth:  nil,
		configDir: filepath.Clean(configDir),
	}
	r.PopulateAuthFromFileList()
	// if we haven't found appropriate secrets try anonymous
	// its not clear if this is what the user wants so we need to log it and let them decide
	if r.PullAuth == nil {
		r.PullAuth = authn.Anonymous
		logger.Info("no secrets for pull found, default to Anonymous")
	}
	if r.PushAuth == nil {
		r.PushAuth = authn.Anonymous
		logger.Info("no secrets for push found, default to Anonymous")
	}
	return r
}

func (r *repoAuth) PopulateAuthFromFileList() {
	// not giving any secrets should short cuircuit the whole process
	if r.configDir == "" {
		return
	}
	logger.Info("walking the tree looking for secrets", "dir", r.configDir)
	// otherwise lets hunt for repo secrets!
	err := filepath.WalkDir(r.configDir, r.authDirFileHandler)
	if err != nil {
		logger.Info("no secret found, default to Anonymous", "error", err)
		// we could return the error and die, but we want to be tolerant of secrets in the wrong format etc
		return
	}
}

func (r *repoAuth) authDirFileHandler(path string, d fs.DirEntry, err error) error {
	if err != nil {
		//theres an error in WalkDir (either fs.Stat or Readdir)
		//not much we can do except log it, and move on
		logger.Info("error walking tree", "filename", path, "error", err)
		return nil
	}
	if path == r.configDir {
		return nil
	}
	if strings.Contains(path, "..") {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	if d.IsDir() {
		return nil
	}
	logger.Info("checking auth file ", "filename", path)
	err = r.getAuthFromSingleFile(path)
	if err != nil {
		logger.Info("unable to read auth file", "filename", path, "error=", err)
	}

	return nil
}

func (r *repoAuth) getAuthFromSingleFile(secretFile string) error {
	jsonFile, err := os.Open(secretFile)
	if err != nil {
		return fmt.Errorf("unable to open secret file %s: %v", secretFile, err)
	}
	// defer the closing of our jsonFile so that we can parse it later o
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return fmt.Errorf("error reading secret file %s: %v", secretFile, err)
	}

	var result map[string]json.RawMessage
	var allAuthConfigs map[string]authn.AuthConfig

	err = json.Unmarshal(byteValue, &result)
	if err != nil {
		return fmt.Errorf("error unmarshalling %s: %v", secretFile, err)
	}

	// either our secret file is a map containing a map of repo secrets
	// or its just a straight map of secrets, so we need some juggling to cope
	if result["auths"] != nil {
		err = json.Unmarshal(result["auths"], &allAuthConfigs)
	} else {
		err = json.Unmarshal(byteValue, &allAuthConfigs)
	}
	if err != nil {
		return fmt.Errorf("error unmarshalling file to authconfig %s: %v", secretFile, err)
	}

	if _, ok := allAuthConfigs[r.pullRepo]; ok {
		r.PullAuth = authn.FromConfig(allAuthConfigs[r.pullRepo])
		logger.Info("Found secret", "pull registry", r.pullRepo)
	}
	if _, ok := allAuthConfigs[r.pushRepo]; ok {
		r.PushAuth = authn.FromConfig(allAuthConfigs[r.pushRepo])
		logger.Info("Found secret", "push registry", r.pushRepo)
	}
	return nil
}

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
			return fmt.Errorf("error signing file %s: %v", canonfilename, err)
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
	var secretDir string
	var extractionDir string
	var filesList string
	var privKeyFile string
	var pubKeyFile string
	var nopush bool
	var insecurePull bool
	var skipTlsVerifyPull bool
	var insecurePush bool
	var skipTlsVerifyPush bool

	logger = klogr.New()

	flag.StringVar(&unsignedImageName, "unsignedimage", "", "name of the image to sign")
	flag.StringVar(&signedImageName, "signedimage", "", "name of the signed image to produce")
	flag.StringVar(&filesList, "filestosign", "", "colon seperated list of kmods to sign")
	flag.StringVar(&privKeyFile, "key", "", "path to file containing private key for signing")
	flag.StringVar(&pubKeyFile, "cert", "", "path to file containing public key for signing")
	flag.StringVar(&secretDir, "secretdir", "", "path to directory containing credentials for pushing images")
	flag.BoolVar(&nopush, "no-push", false, "do not push the resulting image")

	flag.BoolVar(&insecurePull, "insecure-pull", false, "images can be pulled from an insecure (plain HTTP) registry")
	flag.BoolVar(&skipTlsVerifyPull, "skip-tls-verify-pull", false, "do not check TLS certs on pull")
	flag.BoolVar(&insecurePush, "insecure", false, "built images can be pushed to an insecure (plain HTTP) registry")
	flag.BoolVar(&skipTlsVerifyPush, "skip-tls-verify", false, "do not check TLS certs on push")

	flag.Parse()

	checkArg(&unsignedImageName, "unsignedimage", "")
	checkArg(&signedImageName, "signedimage", unsignedImageName+"signed")
	checkArg(&filesList, "filestosign", "")
	checkArg(&privKeyFile, "key", "")
	checkArg(&pubKeyFile, "cert", "")
	checkArg(&secretDir, "pullsecret", "")
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

	a := NewRepoAuth(secretDir, strings.Split(unsignedImageName, "/")[0], strings.Split(signedImageName, "/")[0])

	r := registry.NewRegistry()

	img, err := r.GetImageByName(unsignedImageName, a.PullAuth, insecurePull, skipTlsVerifyPull)
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
		// write the image back to the name:tag set via the args
		err := r.WriteImageByName(signedImageName, signedImage, a.PushAuth, insecurePush, skipTlsVerifyPush)
		if err != nil {
			die(8, "failed to write signed image", err)
		}
		// we're done successfully, so we need a nice friendly message to say that
		logger.Info("Pushed image back to repo", "image", signedImageName)
	}
	os.Exit(0)
}
