package worker

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/golang/mock/gomock"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

type fileExistsMatcher struct {
	isDir bool
}

func ExistAsFile(directory bool) types.GomegaMatcher {
	return &fileExistsMatcher{isDir: directory}
}

func (fem *fileExistsMatcher) Match(actual interface{}) (success bool, err error) {
	path, ok := actual.(string)
	if !ok {
		return false, errors.New("BeARegularFile expects a file path")
	}

	fi, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("error running stat(%q): %v", path, err)
	}

	return fem.isDir == fi.IsDir(), nil
}

func (fem *fileExistsMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected %q to be a file (directory: %t)", actual, fem.isDir)
}

func (fem *fileExistsMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected %q not to be a directory (directory: %t)", actual, fem.isDir)
}

var _ = Describe("worker_LoadKmod", func() {
	var (
		ip *MockImagePuller
		mr *MockModprobeRunner
		w  Worker
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		ip = NewMockImagePuller(ctrl)
		mr = NewMockModprobeRunner(ctrl)
		w = NewWorker(ip, mr, GinkgoLogr)
	})

	ctx := context.TODO()

	const (
		dirName    = "/dir"
		imageName  = "some-image-name"
		moduleName = "test"
	)

	It("should return an error if the image could not be pulled", func() {
		ip.
			EXPECT().
			PullAndExtract(ctx, imageName, false).
			Return(PullResult{}, errors.New("random error"))

		Expect(
			w.LoadKmod(ctx, &v1beta1.ModuleConfig{ContainerImage: imageName}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if modprobe failed", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.EXPECT().Run(ctx, "-vd", dirName, moduleName).Return(errors.New("random error")),
		)

		Expect(
			w.LoadKmod(ctx, &cfg),
		).To(
			HaveOccurred(),
		)
	})

	It("should remove the in-tree module if configured", func() {
		const inTreeModuleToRemove = "intree"

		cfg := v1beta1.ModuleConfig{
			ContainerImage:       imageName,
			InTreeModuleToRemove: inTreeModuleToRemove,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.EXPECT().Run(ctx, "-rv", inTreeModuleToRemove),
			mr.EXPECT().Run(ctx, "-vd", dirName, moduleName),
		)

		Expect(
			w.LoadKmod(ctx, &cfg),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should use rawArgs if they are defined", func() {
		rawArgs := []string{"a", "b", "c"}

		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				RawArgs: &v1beta1.ModprobeArgs{Load: rawArgs},
			},
		}

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.EXPECT().Run(ctx, ToInterfaceSlice(rawArgs)...),
		)

		Expect(
			w.LoadKmod(ctx, &cfg),
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

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.
				EXPECT().
				Run(ctx, "-vd", dirName, "a", "b", "c", moduleName, "key0=value0", "key1=value1"),
		)

		Expect(
			w.LoadKmod(ctx, &cfg),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("worker_UnloadKmod", func() {
	var (
		ip *MockImagePuller
		mr *MockModprobeRunner
		w  Worker
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		ip = NewMockImagePuller(ctrl)
		mr = NewMockModprobeRunner(ctrl)
		w = NewWorker(ip, mr, GinkgoLogr)
	})

	ctx := context.TODO()

	const (
		dirName    = "/dir"
		imageName  = "some-image-name"
		moduleName = "test"
	)

	It("should return an error if the image could not be pulled", func() {
		ip.
			EXPECT().
			PullAndExtract(ctx, imageName, false).
			Return(PullResult{}, errors.New("random error"))

		Expect(
			w.UnloadKmod(ctx, &v1beta1.ModuleConfig{ContainerImage: imageName}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if modprobe failed", func() {
		cfg := v1beta1.ModuleConfig{
			ContainerImage: imageName,
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: moduleName,
				DirName:    dirName,
			},
		}

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.EXPECT().Run(ctx, "-rvd", dirName, moduleName).Return(errors.New("random error")),
		)

		Expect(
			w.UnloadKmod(ctx, &cfg),
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

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.EXPECT().Run(ctx, ToInterfaceSlice(rawArgs)...),
		)

		Expect(
			w.UnloadKmod(ctx, &cfg),
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

		gomock.InOrder(
			ip.EXPECT().PullAndExtract(ctx, imageName, false),
			mr.
				EXPECT().
				Run(ctx, "-rvd", dirName, "a", "b", "c", moduleName),
		)

		Expect(
			w.UnloadKmod(ctx, &cfg),
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
