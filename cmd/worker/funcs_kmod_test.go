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
		"should try and set firmware_class.path if the flag is defined",
		func(flagValue *string) {
			cfg := &kmmv1beta1.ModuleConfig{}
			ctx := context.TODO()

			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cmd.Flags().String(worker.FlagFirmwareClassPath, "", "")

			readConfig := ch.EXPECT().ReadConfigFile(configPath).Return(cfg, nil)
			loadKmod := wo.EXPECT().LoadKmod(ctx, cfg).After(readConfig)

			if flagValue != nil {
				Expect(
					cmd.Flags().Set(worker.FlagFirmwareClassPath, *flagValue),
				).NotTo(
					HaveOccurred(),
				)

				setFirmwarePath := wo.EXPECT().SetFirmwareClassPath(*flagValue)
				setFirmwarePath.After(readConfig)
				loadKmod.After(setFirmwarePath)
			}

			Expect(
				kmodLoadFunc(cmd, []string{configPath}),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("flag not defined", nil),
		Entry("flag defined and empty", pointer.String("")),
		Entry("flag defined and not empty", pointer.String("/some/path")),
	)
})
