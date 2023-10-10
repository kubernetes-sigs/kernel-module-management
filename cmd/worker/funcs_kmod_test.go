package main

import (
	"context"
	"errors"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/pointer"
)

var _ = Describe("kmodLoadFunc", func() {
	const configPath = "/some/path"

	var (
		ch *worker.MockConfigHelper
		wo *worker.MockWorker
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		ch = worker.NewMockConfigHelper(ctrl)
		configHelper = ch
		wo = worker.NewMockWorker(ctrl)
		w = wo
	})

	AfterEach(func() {
		configHelper = worker.NewConfigHelper()
		w = nil
	})

	It("should return an error if we cannot read the config", func() {

		ch.EXPECT().ReadConfigFile(configPath).Return(nil, errors.New("some error"))

		Expect(
			kmodLoadFunc(&cobra.Command{}, []string{configPath}),
		).To(
			HaveOccurred(),
		)
	})

	DescribeTable(
		"set firmware class and firmware mount path if defined",
		func(flagFirmwareClassPathValue, flagFirmwareMountPath *string) {
			cfg := &kmmv1beta1.ModuleConfig{}
			ctx := context.TODO()

			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cmd.Flags().String(worker.FlagFirmwareClassPath, "", "")
			cmd.Flags().String(worker.FlagFirmwareMountPath, "", "")

			readConfig := ch.EXPECT().ReadConfigFile(configPath).Return(cfg, nil)
			var loadKmod *gomock.Call
			if flagFirmwareMountPath != nil {
				Expect(
					cmd.Flags().Set(worker.FlagFirmwareMountPath, *flagFirmwareMountPath),
				).NotTo(
					HaveOccurred(),
				)
				loadKmod = wo.EXPECT().LoadKmod(ctx, cfg, *flagFirmwareMountPath).After(readConfig)
			} else {
				loadKmod = wo.EXPECT().LoadKmod(ctx, cfg, "").After(readConfig)
			}

			if flagFirmwareClassPathValue != nil {
				Expect(
					cmd.Flags().Set(worker.FlagFirmwareClassPath, *flagFirmwareClassPathValue),
				).NotTo(
					HaveOccurred(),
				)

				setFirmwarePath := wo.EXPECT().SetFirmwareClassPath(*flagFirmwareClassPathValue)
				setFirmwarePath.After(readConfig)
				loadKmod.After(setFirmwarePath)
			}

			Expect(
				kmodLoadFunc(cmd, []string{configPath}),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("flags not defined", nil, nil),
		Entry("class path defined and empty, mount path not defined", pointer.String(""), nil),
		Entry("class path defined and not empty, mount path not defined", pointer.String("/some/path"), nil),
		Entry("class path defined and empty, mount path defined", pointer.String(""), pointer.String("some mount path")),
		Entry("class path defined and not empty, mount path defined", pointer.String("/some/path"), pointer.String("some mount path")),
	)
})
