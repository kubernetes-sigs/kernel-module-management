package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("worker_LoadKmod", func() {
	var (
		fh       *utils.MockFSHelper
		mr       *MockModprobeRunner
		w        Worker
		imageDir string
		hostDir  string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		fh = utils.NewMockFSHelper(ctrl)
		mr = NewMockModprobeRunner(ctrl)
		w = NewWorker(mr, fh, GinkgoLogr)

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

	It("should remove present-on-host in-tree module if configured", func() {
		inTreeModulesToRemove := []string{"intree1", "intree2", "intree3", "intree4"}

		cfg := v1beta1.ModuleConfig{
			ContainerImage:        imageName,
			InTreeModulesToRemove: inTreeModulesToRemove,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			fh.EXPECT().FileExists("/lib/modules", "^intree1.ko").Return(true, nil),
			fh.EXPECT().FileExists("/lib/modules", "^intree2.ko").Return(false, nil),
			fh.EXPECT().FileExists("/lib/modules", "^intree3.ko").Return(true, nil),
			fh.EXPECT().FileExists("/lib/modules", "^intree4.ko").Return(false, fmt.Errorf("some error")),
			mr.EXPECT().Run(ctx, "-rv", "intree1", "intree3"),
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
			fh.EXPECT().FileExists("/lib/modules", "^intreeToRemove.ko").Return(true, nil),
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
		mr       *MockModprobeRunner
		fh       *utils.MockFSHelper
		w        Worker
		imageDir string
		hostDir  string
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		mr = NewMockModprobeRunner(ctrl)
		fh = utils.NewMockFSHelper(ctrl)
		w = NewWorker(mr, fh, GinkgoLogr)
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

		mr.EXPECT().Run(ctx, "-rvd", filepath.Join(sharedFilesDir, dirName), moduleName)
		fh.EXPECT().RemoveSrcFilesFromDst(filepath.Join(sharedFilesDir, cfg.Modprobe.FirmwarePath), hostDir).Return(nil)

		Expect(
			w.UnloadKmod(ctx, &cfg, hostDir),
		).NotTo(
			HaveOccurred(),
		)
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
