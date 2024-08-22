package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("worker_LoadKmod", func() {
	var (
		im       *MockImageMounter
		mr       *MockModprobeRunner
		w        Worker
		imageDir string
		hostDir  string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		im = NewMockImageMounter(ctrl)
		mr = NewMockModprobeRunner(ctrl)
		w = NewWorker(im, mr, GinkgoLogr)

		var err error
		imageDir, err = os.MkdirTemp("", "imageDir")
		Expect(err).Should(BeNil())
		hostDir, err = os.MkdirTemp("", "hostMappedDir")
		Expect(err).Should(BeNil())
	})

	AfterEach(func() {
		err := os.RemoveAll(imageDir)
		Expect(err).Should(BeNil())
		err = os.RemoveAll(hostDir)
		Expect(err).Should(BeNil())
	})

	ctx := context.TODO()

	const (
		dirName    = "/dir"
		imageName  = "some-image-name"
		moduleName = "test"
	)

	It("should return an error if modprobe failed", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		mr.EXPECT().Run(ctx, "-vd", filepath.Join(sharedFilesDir, dirName), moduleName).Return(errors.New("random error"))

		Expect(
			w.LoadKmod(ctx, &cfg, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should remove the in-tree module if configured", func() {
		inTreeModulesToRemove := []string{"intree1", "intree2"}

		cfg := v1beta1.ModuleConfig{
			ContainerImage:        imageName,
			InTreeModulesToRemove: inTreeModulesToRemove,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			mr.EXPECT().Run(ctx, "-rv", "intree1", "intree2"),
			mr.EXPECT().Run(ctx, "-vd", filepath.Join(sharedFilesDir, dirName), moduleName),
		)

		Expect(
			w.LoadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should use deprecated InTreeModuleToRemove if configured", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage:        imageName,
			InTreeModuleToRemove:  "intreeToRemove",
			InTreeModulesToRemove: nil,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			mr.EXPECT().Run(ctx, "-rv", "intreeToRemove"),
			mr.EXPECT().Run(ctx, "-vd", filepath.Join(sharedFilesDir, dirName), moduleName),
		)

		Expect(
			w.LoadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should copy all the firmware files/directories if configured", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName:   moduleName,
				DirName:      dirName,
				FirmwarePath: "/firmwareDir",
			},
		}

		err := os.MkdirAll(filepath.Join(sharedFilesDir, "firmwareDir", "binDir"), 0750)
		Expect(err).Should(BeNil())
		err = os.WriteFile(filepath.Join(sharedFilesDir, "firmwareDir", "firwmwareFile1"), []byte("some data 1"), 0660)
		Expect(err).Should(BeNil())
		err = os.WriteFile(filepath.Join(sharedFilesDir, "firmwareDir", "binDir", "firwmwareFile2"), []byte("some data 2"), 0660)
		Expect(err).Should(BeNil())

		mr.EXPECT().Run(ctx, "-vd", filepath.Join(sharedFilesDir, dirName), moduleName)

		Expect(
			w.LoadKmod(ctx, &cfg, hostDir),
		).NotTo(
			HaveOccurred(),
		)
		_, err = os.Stat(hostDir + "/binDir")
		Expect(err).Should(BeNil())
		_, err = os.Stat(hostDir + "/binDir/firwmwareFile2")
		Expect(err).Should(BeNil())
		_, err = os.Stat(hostDir + "/firwmwareFile1")
		Expect(err).Should(BeNil())
	})

	It("should use rawArgs if they are defined", func() {
		rawArgs := []string{"a", "b", "c"}

		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				RawArgs: &v1beta1.ModprobeArgs{Load: rawArgs},
			},
		}

		mr.EXPECT().Run(ctx, ToInterfaceSlice(rawArgs)...)

		Expect(
			w.LoadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should use all modprobe settings", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				Parameters: []string{"key0=value0", "key1=value1"},
				DirName:    dirName,
				Args:       &v1beta1.ModprobeArgs{Load: []string{"a", "b", "c"}},
			},
		}

		mr.EXPECT().Run(ctx, "-vd", filepath.Join(sharedFilesDir, dirName), "a", "b", "c", moduleName, "key0=value0", "key1=value1")

		Expect(
			w.LoadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("worker_SetFirmwareClassPath", func() {
	w := NewWorker(nil, nil, GinkgoLogr)

	AfterEach(func() {
		firmwareClassPathLocation = FirmwareClassPathLocation
	})

	It("should return an error if the sysfile does not exist", func() {
		firmwareClassPathLocation = "/non/existent/path"

		Expect(
			w.SetFirmwareClassPath("some value"),
		).To(
			HaveOccurred(),
		)
	})

	DescribeTable(
		"should work as expected",
		func(oldValue string) {
			firmwareClassPathLocation = filepath.Join(GinkgoT().TempDir(), "firmwareClassPath")

			Expect(
				os.WriteFile(firmwareClassPathLocation, []byte(oldValue), 0666),
			).NotTo(
				HaveOccurred(),
			)

			const value = "new value"

			Expect(
				w.SetFirmwareClassPath(value),
			).NotTo(
				HaveOccurred(),
			)

			Expect(
				os.ReadFile(firmwareClassPathLocation),
			).To(
				Equal([]byte(value)),
			)
		},
		Entry(nil, ""),
		Entry(nil, "old value not empty"),
	)
})

var _ = Describe("worker_UnloadKmod", func() {
	var (
		im       *MockImageMounter
		mr       *MockModprobeRunner
		w        Worker
		imageDir string
		hostDir  string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		im = NewMockImageMounter(ctrl)
		mr = NewMockModprobeRunner(ctrl)
		w = NewWorker(im, mr, GinkgoLogr)
		var err error
		imageDir, err = os.MkdirTemp("", "imageDir")
		Expect(err).Should(BeNil())
		hostDir, err = os.MkdirTemp("", "hostMappedDir")
		Expect(err).Should(BeNil())
	})

	AfterEach(func() {
		err := os.RemoveAll(imageDir)
		Expect(err).Should(BeNil())
		err = os.RemoveAll(hostDir)
		Expect(err).Should(BeNil())
	})

	ctx := context.TODO()

	const (
		dirName    = "/dir"
		imageName  = "some-image-name"
		moduleName = "test"
	)

	It("should return an error if modprobe failed", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		mr.EXPECT().Run(ctx, "-rvd", filepath.Join(sharedFilesDir, dirName), moduleName).Return(errors.New("random error"))

		Expect(
			w.UnloadKmod(ctx, &cfg, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should use rawArgs if they are defined", func() {
		rawArgs := []string{"a", "b", "c"}

		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				RawArgs: &v1beta1.ModprobeArgs{Unload: rawArgs},
			},
		}

		mr.EXPECT().Run(ctx, ToInterfaceSlice(rawArgs)...)

		Expect(
			w.UnloadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should use all modprobe settings", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
				Args:       &v1beta1.ModprobeArgs{Unload: []string{"a", "b", "c"}},
			},
		}

		mr.EXPECT().Run(ctx, "-rvd", filepath.Join(sharedFilesDir, dirName), "a", "b", "c", moduleName)

		Expect(
			w.UnloadKmod(ctx, &cfg, ""),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should remove all firmware file only", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName:   moduleName,
				DirName:      dirName,
				FirmwarePath: "/firmwareDir",
			},
		}

		// prepare the image firmware directories + files
		err := os.MkdirAll(imageDir+"/"+"firmwareDir/binDir", 0750)
		Expect(err).Should(BeNil())
		err = os.WriteFile(imageDir+"/"+"firmwareDir/firmwareFile1", []byte("some data 1"), 0660)
		Expect(err).Should(BeNil())
		err = os.WriteFile(imageDir+"/"+"firmwareDir/binDir/firmwareFile2", []byte("some data 2"), 0660)
		Expect(err).Should(BeNil())

		// prepare the mapped host firmware directories + files
		err = os.MkdirAll(hostDir+"/binDir", 0750)
		Expect(err).Should(BeNil())
		err = os.WriteFile(hostDir+"/firmwareFile1", []byte("some data 1"), 0660)
		Expect(err).Should(BeNil())
		err = os.WriteFile(hostDir+"/binDir/firmwareFile2", []byte("some data 2"), 0660)
		Expect(err).Should(BeNil())

		mr.EXPECT().Run(ctx, "-rvd", filepath.Join(sharedFilesDir, dirName), moduleName)

		Expect(
			w.UnloadKmod(ctx, &cfg, hostDir),
		).NotTo(
			HaveOccurred(),
		)

		// check only the files deletion
		_, err = os.Stat(hostDir + "/binDir")
		Expect(err).Should(BeNil())
		_, err = os.Stat(hostDir + "/binDir/firwmwareFile2")
		Expect(err).NotTo(BeNil())
		_, err = os.Stat(hostDir + "/firwmwareFile1")
		Expect(err).NotTo(BeNil())
	})
})

func ToInterfaceSlice[T any](s []T) []interface{} {
	GinkgoHelper()

	r := make([]interface{}, 0, len(s))

	for i := 0; i < len(s); i++ {
		r = append(r, s[i])
	}

	return r
}
