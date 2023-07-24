package nmc

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/golang/mock/gomock"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Get", func() {
	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		ctx       context.Context
		nmcHelper Helper
		nmcName   string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ctx = context.Background()
		nmcHelper = NewHelper(clnt)
		nmcName = "some name"
	})

	It("success", func() {
		nsn := types.NamespacedName{Name: nmcName}
		clnt.EXPECT().Get(ctx, nsn, gomock.Any()).DoAndReturn(
			func(_ interface{}, _ interface{}, nm *kmmv1beta1.NodeModulesConfig, _ ...ctrlclient.GetOption) error {
				nm.ObjectMeta = metav1.ObjectMeta{Name: nmcName}
				return nil
			},
		)

		res, err := nmcHelper.Get(ctx, nmcName)

		Expect(err).NotTo(HaveOccurred())
		Expect(res.Name).To(Equal(nmcName))
	})

	It("error", func() {
		clnt.EXPECT().Get(ctx, types.NamespacedName{Name: nmcName}, gomock.Any()).Return(fmt.Errorf("some error"))

		_, err := nmcHelper.Get(ctx, nmcName)

		Expect(err).To(HaveOccurred())
	})

	It("no error, nmc does not exists", func() {
		clnt.EXPECT().Get(ctx, types.NamespacedName{Name: nmcName}, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, nmcName))

		res, err := nmcHelper.Get(ctx, nmcName)

		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeNil())
	})

})

var _ = Describe("SetNMCAsDesired", func() {
	var (
		ctx       context.Context
		nmcHelper Helper
	)

	BeforeEach(func() {
		ctx = context.Background()
		nmcHelper = NewHelper(nil)
	})

	namespace := "test_namespace"
	name := "test_name"

	nmc := kmmv1beta1.NodeModulesConfig{}

	It("adding new module", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{Name: "some name 1", Namespace: "some namespace 1"},
			{Name: "some name 2", Namespace: "some namespace 2"},
		}

		moduleConfig := kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "in-tree-module"}

		err := nmcHelper.SetNMCAsDesired(ctx, &nmc, namespace, name, &moduleConfig)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(3))
		Expect(nmc.Spec.Modules[2].Config.InTreeModuleToRemove).To(Equal("in-tree-module"))
	})

	It("changing existing module config", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				Name:      "some name 1",
				Namespace: "some namespace 1",
			},
			{
				Name:      name,
				Namespace: namespace,
				Config:    kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module"},
			},
		}

		moduleConfig := kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "in-tree-module"}

		err := nmcHelper.SetNMCAsDesired(ctx, &nmc, namespace, name, &moduleConfig)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(2))
		Expect(nmc.Spec.Modules[1].Config.InTreeModuleToRemove).To(Equal("in-tree-module"))
	})

	It("deleting non-existent module", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				Name:      "some name 1",
				Namespace: "some namespace 1",
				Config:    kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-1"},
			},
			{
				Name:      "some name 2",
				Namespace: "some namespace 2",
				Config:    kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-2"},
			},
		}

		err := nmcHelper.SetNMCAsDesired(ctx, &nmc, namespace, name, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(2))
		Expect(nmc.Spec.Modules[0].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-1"))
		Expect(nmc.Spec.Modules[1].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-2"))
	})

	It("deleting existing module", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				Name:      "some name 1",
				Namespace: "some namespace 1",
				Config:    kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-1"},
			},
			{
				Name:      name,
				Namespace: namespace,
				Config:    kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-2"},
			},
		}

		err := nmcHelper.SetNMCAsDesired(ctx, &nmc, namespace, name, nil)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(1))
		Expect(nmc.Spec.Modules[0].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-1"))
	})
})

var _ = Describe("GetModuleEntry", func() {
	var (
		nmcHelper Helper
	)

	BeforeEach(func() {
		nmcHelper = NewHelper(nil)
	})

	It("empty module list", func() {
		nmc := kmmv1beta1.NodeModulesConfig{
			Spec: kmmv1beta1.NodeModulesConfigSpec{},
		}

		res, _ := nmcHelper.GetModuleEntry(&nmc, "namespace", "name")

		Expect(res).To(BeNil())
	})

	It("module missing from the list", func() {
		nmc := kmmv1beta1.NodeModulesConfig{
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{Name: "some name 1", Namespace: "some namespace 1"},
					{Name: "some name 2", Namespace: "some namespace 2"},
				},
			},
		}

		res, _ := nmcHelper.GetModuleEntry(&nmc, "namespace", "name")

		Expect(res).To(BeNil())
	})

	It("module present", func() {
		nmc := kmmv1beta1.NodeModulesConfig{
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{Name: "some name 1", Namespace: "some namespace 1"},
					{Name: "some name 2", Namespace: "some namespace 2"},
				},
			},
		}

		res, index := nmcHelper.GetModuleEntry(&nmc, "some namespace 1", "some name 1")

		Expect(res.Name).To(Equal("some name 1"))
		Expect(res.Namespace).To(Equal("some namespace 1"))
		Expect(index).To(Equal(0))
	})
})
