package nmc

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"go.uber.org/mock/gomock"
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

	It("nmc does not exists", func() {
		clnt.EXPECT().Get(ctx, types.NamespacedName{Name: nmcName}, gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, nmcName))

		res, err := nmcHelper.Get(ctx, nmcName)

		Expect(err).To(HaveOccurred())
		Expect(err).To(Equal(k8serrors.NewNotFound(schema.GroupResource{}, nmcName)))
		Expect(res).To(BeNil())
	})

})

var _ = Describe("SetModuleConfig", func() {
	var nmcHelper Helper

	BeforeEach(func() {
		nmcHelper = NewHelper(nil)
	})

	namespace := "test_namespace"
	name := "test_name"

	nmc := kmmv1beta1.NodeModulesConfig{}

	It("adding new module", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				ModuleItem: kmmv1beta1.ModuleItem{Name: "some name 1", Namespace: "some namespace 1"},
			},
			{
				ModuleItem: kmmv1beta1.ModuleItem{Name: "some name 2", Namespace: "some namespace 2"},
			},
		}

		moduleConfig := kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "in-tree-module"}

		err := nmcHelper.SetModuleConfig(&nmc, &api.ModuleLoaderData{Name: name, Namespace: namespace}, &moduleConfig)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(3))
		Expect(nmc.Spec.Modules[2].Config.InTreeModuleToRemove).To(Equal("in-tree-module"))
		Expect(nmc.GetLabels()).To(HaveKeyWithValue(utils.GetModuleNMCLabel(namespace, name), ""))
	})

	It("changing existing module config", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      "some name 1",
					Namespace: "some namespace 1",
				},
			},
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      name,
					Namespace: namespace,
				},
				Config: kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module"},
			},
		}

		moduleConfig := kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "in-tree-module"}

		err := nmcHelper.SetModuleConfig(&nmc, &api.ModuleLoaderData{Name: name, Namespace: namespace}, &moduleConfig)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(2))
		Expect(nmc.Spec.Modules[1].Config.InTreeModuleToRemove).To(Equal("in-tree-module"))
		Expect(nmc.GetLabels()).To(HaveKeyWithValue(utils.GetModuleNMCLabel(namespace, name), ""))
	})
})

var _ = Describe("RemoveModuleConfig", func() {
	var nmcHelper Helper

	BeforeEach(func() {
		nmcHelper = NewHelper(nil)
	})

	namespace := "test_namespace"
	name := "test_name"

	nmc := kmmv1beta1.NodeModulesConfig{}

	It("deleting non-existent module", func() {
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      "some name 1",
					Namespace: "some namespace 1",
				},
				Config: kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-1"},
			},
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      "some name 2",
					Namespace: "some namespace 2",
				},
				Config: kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-2"},
			},
		}

		err := nmcHelper.RemoveModuleConfig(&nmc, namespace, name)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(2))
		Expect(nmc.Spec.Modules[0].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-1"))
		Expect(nmc.Spec.Modules[1].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-2"))
		Expect(nmc.GetLabels()).NotTo(HaveKeyWithValue(utils.GetModuleNMCLabel(namespace, name), ""))
	})

	It("deleting existing module", func() {
		nmc.SetLabels(map[string]string{utils.GetModuleNMCLabel(namespace, name): ""})
		nmc.Spec.Modules = []kmmv1beta1.NodeModuleSpec{
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      "some name 1",
					Namespace: "some namespace 1",
				},
				Config: kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-1"},
			},
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Name:      name,
					Namespace: namespace,
				},
				Config: kmmv1beta1.ModuleConfig{InTreeModuleToRemove: "some-in-tree-module-2"},
			},
		}

		err := nmcHelper.RemoveModuleConfig(&nmc, namespace, name)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(nmc.Spec.Modules)).To(Equal(1))
		Expect(nmc.Spec.Modules[0].Config.InTreeModuleToRemove).To(Equal("some-in-tree-module-1"))
		Expect(nmc.GetLabels()).NotTo(HaveKeyWithValue(utils.GetModuleNMCLabel(namespace, name), ""))
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
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "some name 1",
							Namespace: "some namespace 1",
						},
					},
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "some name 2",
							Namespace: "some namespace 2",
						},
					},
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
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "some name 1",
							Namespace: "some namespace 1",
						},
					},
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "some name 2",
							Namespace: "some namespace 2",
						},
					},
				},
			},
		}

		res, index := nmcHelper.GetModuleEntry(&nmc, "some namespace 1", "some name 1")

		Expect(res.Name).To(Equal("some name 1"))
		Expect(res.Namespace).To(Equal("some namespace 1"))
		Expect(index).To(Equal(0))
	})
})

var _ = Describe("RemoveModuleStatus", func() {
	const (
		name      = "test-name"
		namespace = "test-namespace"
	)

	It("should do nothing if the list is nil", func() {
		RemoveModuleStatus(nil, namespace, name)
	})

	It("should do nothing if the list is empty", func() {
		statuses := make([]kmmv1beta1.NodeModuleStatus, 0)

		RemoveModuleStatus(&statuses, namespace, name)
	})

	It("should remove a status if it exists in the list", func() {
		statuses := []kmmv1beta1.NodeModuleStatus{
			{
				ModuleItem: kmmv1beta1.ModuleItem{
					Namespace: namespace,
					Name:      name,
				},
			},
		}

		RemoveModuleStatus(&statuses, namespace, name)

		Expect(statuses).To(BeEmpty())
	})
})

var _ = Describe("SetModuleStatus", func() {
	const (
		name      = "test-name"
		namespace = "test-namespace"
	)

	now := metav1.Now()

	s := kmmv1beta1.NodeModuleStatus{
		ModuleItem: kmmv1beta1.ModuleItem{
			ImageRepoSecret:    nil, // TODO
			Name:               name,
			Namespace:          namespace,
			ServiceAccountName: "sa",
		},
		Config: &kmmv1beta1.ModuleConfig{
			KernelVersion:        "some-kver",
			ContainerImage:       "some-kernel-image",
			InsecurePull:         true,
			InTreeModuleToRemove: "intree",
		},
		LastTransitionTime: &now,
	}

	It("should do nothing if the slice is nil", func() {
		SetModuleStatus(nil, kmmv1beta1.NodeModuleStatus{})
	})

	It("should add an entry nothing if the list is empty", func() {
		statuses := make([]kmmv1beta1.NodeModuleStatus, 0)

		SetModuleStatus(&statuses, s)

		Expect(statuses).To(HaveLen(1))
		Expect(statuses[0]).To(BeComparableTo(s))
	})

	It("should update an entry if it already exists", func() {
		statuses := []kmmv1beta1.NodeModuleStatus{s}

		new := s
		new.InProgress = true

		SetModuleStatus(&statuses, new)

		Expect(statuses).To(HaveLen(1))
		Expect(statuses[0]).To(BeComparableTo(new))
	})
})
