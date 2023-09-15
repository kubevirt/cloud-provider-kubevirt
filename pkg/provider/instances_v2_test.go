package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/provider/mock/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Instances V2", func() {

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

	Describe("Fetching an instance", func() {

		Context("With a node instance-id label set", func() {

			It("Should fetch a vmi by node's instance-id label value", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{namespace: namespace, client: mockClient}

				infraNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "infra-node",
					},
				}

				vmi := kubevirtv1.VirtualMachineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vmiName,
						Namespace: namespace,
					},
					Status: kubevirtv1.VirtualMachineInstanceStatus{
						NodeName: infraNode.Name,
					},
				}

				tenantNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-node-name",
						Labels: map[string]string{
							"node.kubernetes.io/instance-id": "test-vm",
						},
					},
				}

				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						SetArg(2, vmi).
						Times(1),
					mockClient.EXPECT().
						Get(ctx, client.ObjectKey{Name: infraNode.Name}, gomock.AssignableToTypeOf(&corev1.Node{})).
						SetArg(2, infraNode).
						Times(1),
				)

				metadata, err := i.InstanceMetadata(ctx, &tenantNode)
				Expect(err).To(BeNil())
				Expect(metadata.ProviderID).To(Equal("kubevirt://test-vm"))
			})

			It("Should return an error if cannot find a vmi by node's instance-id label value", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{namespace: namespace, client: mockClient}

				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "custom-node-name",
						Labels: map[string]string{
							"node.kubernetes.io/instance-id": "test-vm",
						},
					},
				}

				mockClient.EXPECT().
					Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
					Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
					Times(1)

				metadata, err := i.InstanceMetadata(ctx, &node)
				Expect(err).To(HaveOccurred())
				Expect(metadata).To(BeNil())
			})
		})

		Context("With a node name name value equal to a vmi name", func() {

			It("Should default to last known internal IP when no vmi.status.interfaces default ip exists", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{
					namespace: namespace,
					client:    mockClient,
					config: &InstancesV2Config{
						Enabled:              true,
						ZoneAndRegionEnabled: true,
					},
				}

				infraNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "infra-node",
						Labels: map[string]string{
							corev1.LabelTopologyRegion: "region-a",
							corev1.LabelTopologyZone:   "zone-1",
						},
					},
				}

				vmi := kubevirtv1.VirtualMachineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vmiName,
						Namespace: namespace,
						Annotations: map[string]string{
							kubevirtv1.InstancetypeAnnotation: "highPerformance",
						},
					},
					Status: kubevirtv1.VirtualMachineInstanceStatus{
						Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
							{
								IP:   "10.245.0.1",
								Name: "unknown",
							},
							{
								IP: "10.246.0.1",
							},
						},
						NodeName: infraNode.Name,
					},
				}

				tenantNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiName,
					},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.200.100.1",
							},
						},
					},
				}

				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						SetArg(2, vmi).
						Times(1),
					mockClient.EXPECT().
						Get(ctx, client.ObjectKey{Name: infraNode.Name}, gomock.AssignableToTypeOf(&corev1.Node{})).
						SetArg(2, infraNode).
						Times(1),
				)

				metadata, err := i.InstanceMetadata(ctx, &tenantNode)
				Expect(err).To(BeNil())

				idFn := func(index int, element interface{}) string {
					return strconv.Itoa(index)
				}
				Expect(*metadata).To(MatchAllFields(Fields{
					"ProviderID": Equal("kubevirt://test-vm"),
					"NodeAddresses": MatchAllElementsWithIndex(idFn, Elements{
						"0": MatchAllFields(Fields{
							"Address": Equal("10.200.100.1"),
							"Type":    Equal(corev1.NodeInternalIP),
						}),
					}),
					"InstanceType": Equal("highPerformance"),
					"Region":       Equal("region-a"),
					"Zone":         Equal("zone-1"),
				}))
			})

			It("Should fetch a vmi by node name and return a complete metadata object", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{
					namespace: namespace,
					client:    mockClient,
					config: &InstancesV2Config{
						Enabled:              true,
						ZoneAndRegionEnabled: true,
					},
				}

				infraNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "infra-node",
						Labels: map[string]string{
							corev1.LabelTopologyRegion: "region-a",
							corev1.LabelTopologyZone:   "zone-1",
						},
					},
				}

				vmi := kubevirtv1.VirtualMachineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vmiName,
						Namespace: namespace,
						Annotations: map[string]string{
							kubevirtv1.InstancetypeAnnotation: "highPerformance",
						},
					},
					Status: kubevirtv1.VirtualMachineInstanceStatus{
						Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
							{
								IP:   "10.244.0.1",
								Name: "default",
							},
							{
								IP:   "10.245.0.1",
								Name: "unknown",
							},
							{
								IP: "10.246.0.1",
							},
						},
						NodeName: infraNode.Name,
					},
				}

				tenantNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiName,
					},
					Status: corev1.NodeStatus{
						Addresses: []corev1.NodeAddress{
							{
								Type:    corev1.NodeInternalIP,
								Address: "10.200.100.1",
							},
						},
					},
				}

				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						SetArg(2, vmi).
						Times(1),
					mockClient.EXPECT().
						Get(ctx, client.ObjectKey{Name: infraNode.Name}, gomock.AssignableToTypeOf(&corev1.Node{})).
						SetArg(2, infraNode).
						Times(1),
				)

				metadata, err := i.InstanceMetadata(ctx, &tenantNode)
				Expect(err).To(BeNil())

				idFn := func(index int, element interface{}) string {
					return strconv.Itoa(index)
				}
				Expect(*metadata).To(MatchAllFields(Fields{
					"ProviderID": Equal("kubevirt://test-vm"),
					"NodeAddresses": MatchAllElementsWithIndex(idFn, Elements{
						"0": MatchAllFields(Fields{
							"Address": Equal("10.244.0.1"),
							"Type":    Equal(corev1.NodeInternalIP),
						}),
					}),
					"InstanceType": Equal("highPerformance"),
					"Region":       Equal("region-a"),
					"Zone":         Equal("zone-1"),
				}))
			})

			It("Should fetch a vmi by node name and return a complete metadata object - zone and region disabled", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{
					namespace: namespace,
					client:    mockClient,
					config: &InstancesV2Config{
						Enabled:              true,
						ZoneAndRegionEnabled: false,
					},
				}

				infraNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "infra-node",
						Labels: map[string]string{
							corev1.LabelTopologyRegion: "region-a",
							corev1.LabelTopologyZone:   "zone-1",
						},
					},
				}

				vmi := kubevirtv1.VirtualMachineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      vmiName,
						Namespace: namespace,
						Annotations: map[string]string{
							kubevirtv1.InstancetypeAnnotation: "highPerformance",
						},
					},
					Status: kubevirtv1.VirtualMachineInstanceStatus{
						Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
							{
								IP:   "10.244.0.1",
								Name: "default",
							},
							{
								IP:   "10.245.0.1",
								Name: "unknown",
							},
							{
								IP: "10.246.0.1",
							},
						},
						NodeName: infraNode.Name,
					},
				}

				tenantNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiName,
					},
				}

				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						SetArg(2, vmi).
						Times(1),
				)

				metadata, err := i.InstanceMetadata(ctx, &tenantNode)
				Expect(err).To(BeNil())

				idFn := func(index int, element interface{}) string {
					return strconv.Itoa(index)
				}
				Expect(*metadata).To(MatchAllFields(Fields{
					"ProviderID": Equal("kubevirt://test-vm"),
					"NodeAddresses": MatchAllElementsWithIndex(idFn, Elements{
						"0": MatchAllFields(Fields{
							"Address": Equal("10.244.0.1"),
							"Type":    Equal(corev1.NodeInternalIP),
						}),
					}),
					"InstanceType": Equal("highPerformance"),
					"Region":       Equal(""),
					"Zone":         Equal(""),
				}))
			})

			It("Should return an error if unable to find a vmi", func() {
				vmiName := "test-vm"
				namespace := "cluster-qwedas"
				i := instancesV2{namespace: namespace, client: mockClient}

				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiName,
					},
				}

				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiName, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
						Times(1),
					mockClient.EXPECT().
						List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), gomock.Any()).
						Times(1),
				)

				_, err := i.InstanceMetadata(ctx, &node)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("With a node name value equal to a vmi hostname", func() {

			It("Should try to fetch a vmi by hostname", func() {
				vmiName := "test-vm"
				vmiHostname := "test-vm-hostname"
				namespace := "cluster-qwedas"
				i := instancesV2{namespace: namespace, client: mockClient}

				infraNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "infra-node",
					},
				}

				vmis := kubevirtv1.VirtualMachineInstanceList{
					Items: []kubevirtv1.VirtualMachineInstance{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      vmiName,
								Namespace: namespace,
							},
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Hostname: vmiHostname,
							},
							Status: kubevirtv1.VirtualMachineInstanceStatus{
								NodeName: infraNode.Name,
							},
						},
					},
				}

				tenantNode := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiHostname,
					},
				}

				lo := &client.ListOptions{
					Namespace:     namespace,
					FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": tenantNode.Name}),
				}
				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiHostname, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
						Times(1),
					mockClient.EXPECT().
						List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), lo).
						SetArg(1, vmis).
						Times(1),
					mockClient.EXPECT().
						Get(ctx, client.ObjectKey{Name: infraNode.Name}, gomock.AssignableToTypeOf(&corev1.Node{})).
						SetArg(2, infraNode).
						Times(1),
				)

				metadata, err := i.InstanceMetadata(ctx, &tenantNode)
				Expect(err).To(BeNil())
				Expect(metadata.ProviderID).To(Equal("kubevirt://test-vm"))
			})

			It("Should return an error if multiple vmis have the same hostname", func() {
				vmiHostname := "test-vm-hostname"
				namespace := "cluster-qwedas"
				i := instancesV2{namespace: namespace, client: mockClient}

				vmis := kubevirtv1.VirtualMachineInstanceList{
					Items: []kubevirtv1.VirtualMachineInstance{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "vm-a",
								Namespace: namespace,
							},
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Hostname: vmiHostname,
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "vm-b",
								Namespace: namespace,
							},
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Hostname: vmiHostname,
							},
						},
					},
				}
				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmiHostname,
					},
				}

				lo := &client.ListOptions{
					Namespace:     namespace,
					FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": node.Name}),
				}
				gomock.InOrder(
					mockClient.EXPECT().
						Get(ctx, types.NamespacedName{Name: vmiHostname, Namespace: namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
						Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
						Times(1),
					mockClient.EXPECT().
						List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), lo).
						SetArg(1, vmis).
						Times(1),
				)

				_, err := i.InstanceMetadata(ctx, &node)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Checking if instance exist", func() {

		Context("With providerID set", func() {

			It("Should return true if VMI exists", func() {
				vmi := &kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "cluster-test",
				}}
				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmi.Name,
					},
					Spec: corev1.NodeSpec{
						ProviderID: getProviderID(vmi.Name),
					},
				}

				mockClient.EXPECT().
					Get(ctx, types.NamespacedName{Name: vmi.Name, Namespace: vmi.Namespace}, &kubevirtv1.VirtualMachineInstance{}).
					Times(1)

				i := instancesV2{namespace: vmi.Namespace, client: mockClient}
				exists, err := i.InstanceExists(ctx, &node)
				Expect(exists).To(BeTrue())
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should not return an error if VMI does not exist", func() {
				namespace := "cluster-test"

				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vm-b",
					},
					Spec: corev1.NodeSpec{
						ProviderID: getProviderID("vm-b"),
					},
				}

				mockClient.EXPECT().
					Get(ctx, types.NamespacedName{Name: node.Name, Namespace: namespace}, &kubevirtv1.VirtualMachineInstance{}).
					Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")).
					Times(1)

				i := instancesV2{namespace: namespace, client: mockClient}
				exists, err := i.InstanceExists(ctx, &node)
				Expect(exists).To(BeFalse())
				Expect(err).ToNot(HaveOccurred())
			})

			It("Should return an error if provider id is invalid", func() {
				vmi := &kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "cluster-test",
				}}
				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmi.Name,
					},
					Spec: corev1.NodeSpec{
						ProviderID: "invalid-provider-id",
					},
				}

				i := instancesV2{namespace: vmi.Namespace, client: mockClient}
				exists, err := i.InstanceExists(ctx, &node)
				Expect(exists).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})
		})

		Context("Without providerID set", func() {

			It("Should return an error", func() {
				vmi := &kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "cluster-test",
				}}
				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmi.Name,
					},
				}

				i := instancesV2{namespace: vmi.Namespace, client: mockClient}
				exists, err := i.InstanceExists(ctx, &node)
				Expect(exists).To(BeFalse())
				Expect(err).To(HaveOccurred())
			})

		})
	})

	Describe("Checking instance status", func() {

		Context("With VMI shutdown", func() {

			DescribeTable("With VMI phase check", func(phase kubevirtv1.VirtualMachineInstancePhase, shutdown bool) {
				vmi := kubevirtv1.VirtualMachineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "cluster-test",
					},
					Status: kubevirtv1.VirtualMachineInstanceStatus{
						Phase: phase,
					},
				}
				node := corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: vmi.Name,
					},
					Spec: corev1.NodeSpec{
						ProviderID: getProviderID(vmi.Name),
					},
				}

				mockClient.EXPECT().
					Get(ctx, types.NamespacedName{Name: vmi.Name, Namespace: vmi.Namespace}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).
					SetArg(2, vmi).
					Times(1)

				i := instancesV2{namespace: vmi.Namespace, client: mockClient}
				status, err := i.InstanceShutdown(ctx, &node)
				Expect(status).To(Equal(shutdown))
				if phase == kubevirtv1.Unknown {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).ToNot(HaveOccurred())
				}
			},
				Entry(fmt.Sprintf("Should return true with '%s' status phase", kubevirtv1.Succeeded), kubevirtv1.Succeeded, true),
				Entry(fmt.Sprintf("Should return true with '%s' status phase", kubevirtv1.Failed), kubevirtv1.Failed, true),
				Entry(fmt.Sprintf("Should return true with '%s' status phase", kubevirtv1.Unknown), kubevirtv1.Unknown, true),
				Entry("Should return false with any different phase", kubevirtv1.VirtualMachineInstancePhase(""), false),
			)

		})

	})

})
