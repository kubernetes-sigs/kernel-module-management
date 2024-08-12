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
	"k8s.io/utils/ptr"
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
		func(flagFirmwarePath *string) {
			cfg := &kmmv1beta1.ModuleConfig{}
			ctx := context.TODO()

			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			cmd.Flags().String(worker.FlagFirmwarePath, "", "")

			if flagFirmwarePath != nil {
				Expect(
					cmd.Flags().Set(worker.FlagFirmwarePath, *flagFirmwarePath),
				).NotTo(
					HaveOccurred(),
				)
				gomock.InOrder(
					ch.EXPECT().ReadConfigFile(configPath).Return(cfg, nil),
					wo.EXPECT().SetFirmwareClassPath(*flagFirmwarePath),
					wo.EXPECT().LoadKmod(ctx, cfg, *flagFirmwarePath),
				)
			} else {
				gomock.InOrder(
					ch.EXPECT().ReadConfigFile(configPath).Return(cfg, nil),
					wo.EXPECT().LoadKmod(ctx, cfg, ""),
				)
			}

			Expect(
				kmodLoadFunc(cmd, []string{configPath}),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("fimrwarePath not defined", nil),
		Entry("fimrwarePath path defined and empty", ptr.To("")),
		Entry("firmwarePath defined", ptr.To("/some/path")),
	)
})
