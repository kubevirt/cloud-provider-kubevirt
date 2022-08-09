package provider

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/provider/mock/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Instance getters", func() {

	var (
		mockClient *mockclient.MockClient
		ctrl       *gomock.Controller
		ctx        context.Context
	)

	BeforeEach(func() {
		ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
		mockClient = mockclient.NewMockClient(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("With getting instance by vmi name", func() {

		It("Should return instance if found", func() {
			vmi := kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "cluster-test",
			}}

			mockClient.EXPECT().
				Get(ctx, types.NamespacedName{Name: vmi.Name, Namespace: vmi.Namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
				SetArg(2, vmi).
				Times(1)

			instance, err := InstanceByVMIName(vmi.Name).Get(ctx, mockClient, vmi.Namespace)
			Expect(err).To(BeNil())
			Expect(instance.Name).To(Equal(vmi.Name))
		})

		It("Should return an error if not found", func() {
			vmi := kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "cluster-test",
			}}

			mockClient.EXPECT().
				Get(ctx, types.NamespacedName{Name: vmi.Name, Namespace: vmi.Namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
				Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
				Times(1)

			instance, err := InstanceByVMIName(vmi.Name).Get(ctx, mockClient, vmi.Namespace)
			Expect(err).To(HaveOccurred())
			Expect(instance).To(BeNil())
		})

	})

	Context("With getting an instance by vmi hostname", func() {

		It("Should return instance if found one instance", func() {
			vmis := kubevirtv1.VirtualMachineInstanceList{
				Items: []kubevirtv1.VirtualMachineInstance{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vm-a",
							Namespace: "cluster-test",
						},
						Spec: kubevirtv1.VirtualMachineInstanceSpec{
							Hostname: "hostname-a",
						},
					},
				},
			}

			lo := &client.ListOptions{
				Namespace:     vmis.Items[0].Namespace,
				FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": vmis.Items[0].Spec.Hostname}),
			}
			mockClient.EXPECT().
				List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), lo).
				SetArg(1, vmis).
				Times(1)

			instance, err := InstanceByVMIHostname(vmis.Items[0].Spec.Hostname).Get(ctx, mockClient, vmis.Items[0].Namespace)
			Expect(err).To(BeNil())
			Expect(instance.Name).To(Equal(vmis.Items[0].Name))
		})

		It("Should return an error if found multiple instances", func() {
			vmis := kubevirtv1.VirtualMachineInstanceList{
				Items: []kubevirtv1.VirtualMachineInstance{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vm-a",
							Namespace: "cluster-test",
						},
						Spec: kubevirtv1.VirtualMachineInstanceSpec{
							Hostname: "test-hostname",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vm-b",
							Namespace: "cluster-test",
						},
						Spec: kubevirtv1.VirtualMachineInstanceSpec{
							Hostname: "test-hostname",
						},
					},
				},
			}

			lo := &client.ListOptions{
				Namespace:     vmis.Items[0].Namespace,
				FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": vmis.Items[0].Spec.Hostname}),
			}
			mockClient.EXPECT().
				List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), lo).
				SetArg(1, vmis).
				Times(1)

			instance, err := InstanceByVMIHostname(vmis.Items[0].Spec.Hostname).Get(ctx, mockClient, vmis.Items[0].Namespace)
			Expect(err).To(HaveOccurred())
			Expect(instance).To(BeNil())
		})

		It("Should return an error if not found", func() {
			hostname := "test-hostname"
			namespace := "test-cluster"

			lo := &client.ListOptions{
				Namespace:     namespace,
				FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": hostname}),
			}
			mockClient.EXPECT().
				List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), lo).
				Times(1)

			instance, err := InstanceByVMIHostname(hostname).Get(ctx, mockClient, namespace)
			Expect(err).To(HaveOccurred())
			Expect(instance).To(BeNil())
		})

	})
})
