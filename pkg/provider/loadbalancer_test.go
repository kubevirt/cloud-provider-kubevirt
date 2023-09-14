package provider

import (
	"context"
	"errors"
	"fmt"

	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/provider/mock/client"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	lbServiceName      string = "af6ebf1722bb111e9b210d663bd873d9"
	lbServiceNamespace string = "test"
	clusterName        string = "kvcluster"
)

var (
	svcEmptyStatus = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a6fa1a2662ad011e9b210d663bd873d9",
			Namespace: "test",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt(30005)},
			},
		},
	}
	svcHostnameIngress = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a6fa1a6582ad011e9b210d663bd873d9",
			Namespace: "test",
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "192.168.0.36", Hostname: "lb1.example.com"},
					{Hostname: "lb2.example.com"},
				},
			},
		},
	}
	svc4 = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a6fa1a50e2ad011e9b210d663bd873d9",
			Namespace: "test",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt(30005)},
			},
			Selector: map[string]string{
				"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "192.168.0.35"},
				},
			},
		},
	}
	notFoundErr = apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, lbServiceName)
)

func makeLoadBalancerStatus(ips, hostnames []string) *corev1.LoadBalancerStatus {
	status := &corev1.LoadBalancerStatus{}
	lenIps := len(ips)
	lenHostnames := len(hostnames)
	var length int
	if lenIps > lenHostnames {
		length = lenIps
	} else {
		length = lenHostnames
	}
	if length == 0 {
		return status
	}
	ingressList := make([]corev1.LoadBalancerIngress, length)

	for i := 0; i < length; i++ {
		var ip, hostname string
		if i < lenIps {
			ip = ips[i]
		}
		if i < lenHostnames {
			hostname = hostnames[i]
		}
		ingressList[i] = corev1.LoadBalancerIngress{
			IP:       ip,
			Hostname: hostname,
		}
	}
	status.Ingress = ingressList
	return status
}

func cmpLoadBalancerStatuses(a, b *corev1.LoadBalancerStatus) bool {
	if a == b {
		return true
	}

	if (a == nil || b == nil) || (len(a.Ingress) != len(b.Ingress)) {
		return false
	}

	for i, aIngress := range a.Ingress {
		if (aIngress.IP != b.Ingress[i].IP) || (aIngress.Hostname != b.Ingress[i].Hostname) {
			return false
		}
	}

	return true
}

func generateInfraService(tenantSvc *corev1.Service, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbServiceName,
			Namespace: lbServiceNamespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/tenant-service-name":      tenantSvc.Name,
				"cluster.x-k8s.io/tenant-service-namespace": tenantSvc.Namespace,
				"cluster.x-k8s.io/cluster-name":             clusterName,
			},
			Annotations: tenantSvc.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			Ports:                 ports,
			ExternalTrafficPolicy: tenantSvc.Spec.ExternalTrafficPolicy,
			Selector: map[string]string{
				"cluster.x-k8s.io/role":         "worker",
				"cluster.x-k8s.io/cluster-name": clusterName,
			},
		},
	}
}

var _ = Describe("LoadBalancer", func() {

	Context("With getting loadbalancer status", Ordered, func() {

		var (
			c    *mockclient.MockClient
			ctrl *gomock.Controller
			ctx  context.Context
			lb   *loadbalancer
		)

		BeforeAll(func() {
			ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
			c = mockclient.NewMockClient(ctrl)
			lb = &loadbalancer{
				namespace: "test",
				client:    c,
			}
			gomock.InOrder(
				c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a2662ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svcEmptyStatus),
				c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
				c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a6582ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svcHostnameIngress),
				c.EXPECT().Get(ctx, client.ObjectKey{Name: "adoesnotexistink8s", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).Return(apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, "adoesnotexistink8s")),
			)
		})

		DescribeTable("Get loadbalancer", func(serviceUID types.UID, clusterName string, expectedLBStatus *corev1.LoadBalancerStatus, expectedExists bool, expectedError error) {
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: serviceUID}}
			status, exists, err := lb.GetLoadBalancer(ctx, clusterName, svc)
			Expect(cmpLoadBalancerStatuses(status, expectedLBStatus)).Should(BeTrue())
			Expect(exists).Should(Equal(expectedExists))
			if expectedError != nil {
				Expect(err).Should(Equal(expectedError))
			} else {
				Expect(err).NotTo(HaveOccurred())
			}

		},
			Entry("Should return status with no IPs & hostnames", types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", makeLoadBalancerStatus([]string{}, []string{}), true, nil),
			Entry("Should return status with IPs & no hostnames", types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", makeLoadBalancerStatus([]string{"192.168.0.35"}, []string{}), true, nil),
			Entry("Should return status with IPs & hostnames", types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", makeLoadBalancerStatus([]string{"192.168.0.36"}, []string{"lb1.example.com", "lb2.example.com"}), true, nil),
			Entry("Should return with not-exist-status", types.UID("does-not-exist-in-k8s"), "cluster2", nil, false, nil),
		)

		AfterAll(func() {
			ctrl.Finish()
		})
	})

	Context("With getting loadbalancer name", Ordered, func() {

		var (
			c    *mockclient.MockClient
			ctrl *gomock.Controller
			ctx  context.Context
			lb   *loadbalancer
		)

		BeforeAll(func() {
			ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
			c = mockclient.NewMockClient(ctrl)
			lb = &loadbalancer{
				namespace: "test",
				client:    c,
			}
		})

		DescribeTable("Get loadbalancer name", func(serviceUID types.UID, clusterName string, expectedName string) {
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: serviceUID}}
			name := lb.GetLoadBalancerName(ctx, clusterName, svc)
			Expect(name).Should(Equal(expectedName))
		},
			Entry(fmt.Sprintf("Should return loadbalancer with %s name ", "a6fa1a2662ad011e9b210d663bd873d9"), types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", "a6fa1a2662ad011e9b210d663bd873d9"),
			Entry(fmt.Sprintf("Should return loadbalancer with %s name ", "a6fa1a50e2ad011e9b210d663bd873d9"), types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", "a6fa1a50e2ad011e9b210d663bd873d9"),
			Entry(fmt.Sprintf("Should return loadbalancer with %s name ", "a6fa1a6582ad011e9b210d663bd873d9"), types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", "a6fa1a6582ad011e9b210d663bd873d9"),
		)

		AfterAll(func() {
			ctrl.Finish()
		})

	})

	Context("With ensuring loadbalancer", Ordered, func() {

		var (
			c              *mockclient.MockClient
			ctrl           *gomock.Controller
			ctx            context.Context
			lb             *loadbalancer
			tenantService  *corev1.Service
			nodes          []*corev1.Node
			loadBalancerIP string
		)

		BeforeAll(func() {
			ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
			c = mockclient.NewMockClient(ctrl)
			lb = &loadbalancer{
				namespace: "test",
				client:    c,
				config: LoadBalancerConfig{
					CreationPollInterval: pointer.Int(1),
					CreationPollTimeout:  pointer.Int(5),
				},
			}

			tenantService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
					Annotations: map[string]string{
						"annotation-key-1": "annotation-val-1",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001},
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
				},
			}
			nodes = []*corev1.Node{}
			loadBalancerIP = "123.456.7.8"

		})

		It("Should create new Service and poll LoadBalancer service 1 time", func() {
			checkSvcExistErr := notFoundErr
			getCount := 1
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(checkSvcExistErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1)

			for i := 0; i < getCount; i++ {
				infraService2 := infraService1.DeepCopy()
				if i == getCount-1 {
					infraService2.Status = corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: loadBalancerIP,
								},
							},
						},
					}
				}
				c.EXPECT().Get(
					ctx,
					client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
					gomock.AssignableToTypeOf(&corev1.Service{}),
				).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
					infraService2.DeepCopyInto(obj.(*corev1.Service))
				})
			}

			lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).To(BeNil())
			Expect(len(lbStatus.Ingress)).Should(Equal(1))
			Expect(lbStatus.Ingress[0].IP).Should(Equal(loadBalancerIP))

		})

		It("Should create new Service and poll LoadBalancer service 3 times", func() {
			checkSvcExistErr := notFoundErr
			getCount := 3
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(checkSvcExistErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1)

			for i := 0; i < getCount; i++ {
				infraService2 := infraService1.DeepCopy()
				if i == getCount-1 {
					infraService2.Status = corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: loadBalancerIP,
								},
							},
						},
					}
				}
				c.EXPECT().Get(
					ctx,
					client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
					gomock.AssignableToTypeOf(&corev1.Service{}),
				).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
					infraService2.DeepCopyInto(obj.(*corev1.Service))
				})
			}

			lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).To(BeNil())
			Expect(len(lbStatus.Ingress)).Should(Equal(1))
			Expect(lbStatus.Ingress[0].IP).Should(Equal(loadBalancerIP))

		})

		It("Should return an error if service already exist", func() {
			expectedError := errors.New("Test error - check if service already exist")
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(errors.New("Test error - check if service already exist"))

			lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
			Expect(lbStatus).To(BeNil())
		})

		It("Should update existing service with ports changed", func() {
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Update(ctx, infraService1)

			lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).To(BeNil())
			Expect(len(lbStatus.Ingress)).Should(Equal(1))
			Expect(lbStatus.Ingress[0].IP).Should(Equal(loadBalancerIP))
		})

		It("Should update existing service with no ports changed", func() {
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).To(BeNil())
			Expect(len(lbStatus.Ingress)).Should(Equal(1))
			Expect(lbStatus.Ingress[0].IP).Should(Equal(loadBalancerIP))
		})

		It("Should return an error while updating existing service with ports changed ", func() {
			expectedError := errors.New("Test error - update Service")
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Update(ctx, infraService1).Return(errors.New("Test error - update Service"))

			_, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		It("Should return  an error if service creation failed", func() {
			expectedError := errors.New("Test error - Create Service")
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(notFoundErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1).Return(errors.New("Test error - Create Service"))

			_, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		It("Should return and error if polling LoadBalancer service 1-time fails", func() {
			expectedError := errors.New("Test error - poll Service")
			getCount := 1
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(notFoundErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1)

			for i := 0; i < getCount; i++ {
				infraService2 := infraService1.DeepCopy()
				if i == getCount-1 {
					infraService2.Status = corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: loadBalancerIP,
								},
							},
						},
					}
				}
				c.EXPECT().Get(
					ctx,
					client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
					gomock.AssignableToTypeOf(&corev1.Service{}),
				).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
					infraService2.DeepCopyInto(obj.(*corev1.Service))
				}).Return(errors.New("Test error - poll Service"))
			}

			_, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		It("Should return an error if polling LoadBalancer service fails but success before timeout", func() {
			expectedError := errors.New("Test error - poll Service")
			getCount := *lb.config.CreationPollTimeout / *lb.config.CreationPollInterval
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(notFoundErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1)

			for i := 0; i < getCount; i++ {
				infraService2 := infraService1.DeepCopy()
				if i == getCount-1 {
					c.EXPECT().Get(
						ctx,
						client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
						gomock.AssignableToTypeOf(&corev1.Service{}),
					).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
						infraService2.DeepCopyInto(obj.(*corev1.Service))
					}).Return(errors.New("Test error - poll Service"))
				} else {
					c.EXPECT().Get(
						ctx,
						client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
						gomock.AssignableToTypeOf(&corev1.Service{}),
					).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
						infraService2.DeepCopyInto(obj.(*corev1.Service))
					})
				}
			}

			_, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		AfterAll(func() {
			ctrl.Finish()
		})

		It("Should return an error if polling LoadBalancer returns no IPs after some time", func() {
			expectedError := errors.New("timed out waiting for the condition")

			c.EXPECT().
				Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).
				Return(notFoundErr)

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			c.EXPECT().Create(ctx, infraService1)

			infraService2 := infraService1.DeepCopy()
			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraService2.DeepCopyInto(obj.(*corev1.Service))
			}).AnyTimes()

			_, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(MatchError(expectedError))
		})

		AfterAll(func() {
			ctrl.Finish()
		})

	})

	Context("With updating loadbalancer", Ordered, func() {

		var (
			c              *mockclient.MockClient
			ctrl           *gomock.Controller
			ctx            context.Context
			lb             *loadbalancer
			tenantService  *corev1.Service
			nodes          []*corev1.Node
			loadBalancerIP string
		)

		BeforeAll(func() {
			ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
			c = mockclient.NewMockClient(ctrl)

			lb = &loadbalancer{
				namespace: "test",
				client:    c,
				config: LoadBalancerConfig{
					CreationPollInterval: pointer.Int(1),
				},
			}

			tenantService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
					Annotations: map[string]string{
						"annotation-key-1": "annotation-val-1",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001},
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
				},
			}
			nodes = []*corev1.Node{}
			loadBalancerIP = "123.456.7.8"

		})

		It("Should update loadbalancer Service with ports changed", func() {
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)

			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)

			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Update(ctx, infraService1)

			err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("Should update loadbalancer Service with ports not changed", func() {
			port := 30001
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(port)}},
				},
			)

			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)
			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("Should return an error if get service fails", func() {
			expectedError := errors.New("Test error - Get Service")
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Return(errors.New("Test error - Get Service"))

			err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		It("Should return an error if service not found", func() {
			expectedError := notFoundErr
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Return(notFoundErr)

			err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		It("Should return an error if update fails with ports changed", func() {
			expectedError := errors.New("Test error - update Service")
			changedPort := 30002
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: int32(changedPort)}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}
			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
			})

			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)
			infraService1.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}

			c.EXPECT().Update(ctx, infraService1).Return(errors.New("Test error - update Service"))

			err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
			Expect(err).Should(Equal(expectedError))
		})

		AfterAll(func() {
			ctrl.Finish()
		})
	})

	Context("With ensuring loadbalancer is deleted", Ordered, func() {

		var (
			c              *mockclient.MockClient
			ctrl           *gomock.Controller
			ctx            context.Context
			lb             *loadbalancer
			tenantService  *corev1.Service
			loadBalancerIP string
		)

		BeforeAll(func() {
			ctrl, ctx = gomock.WithContext(context.Background(), GinkgoT())
			c = mockclient.NewMockClient(ctrl)

			lb = &loadbalancer{
				namespace: "test",
				client:    c,
				config: LoadBalancerConfig{
					CreationPollInterval: pointer.Int(1),
				},
			}

			tenantService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
					Annotations: map[string]string{
						"annotation-key-1": "annotation-val-1",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001},
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
				},
			}

			loadBalancerIP = "123.456.7.8"
		})

		DescribeTable("Ensure loadbalancer deleted", func(getSvcErr error, deleteSvcErr error, expectedError error) {
			infraServiceExist := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30002}},
				},
			)
			infraServiceExist.Status = corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{
						{
							IP: loadBalancerIP,
						},
					},
				},
			}
			c.EXPECT().Get(
				ctx,
				client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"},
				gomock.AssignableToTypeOf(&corev1.Service{}),
			).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) {
				if getSvcErr == nil {
					infraServiceExist.DeepCopyInto(obj.(*corev1.Service))
				}
			}).Return(getSvcErr)
			if getSvcErr == nil {
				c.EXPECT().Delete(ctx, infraServiceExist).Return(deleteSvcErr)
			}

			err := lb.EnsureLoadBalancerDeleted(ctx, clusterName, tenantService)
			if expectedError == nil {
				Expect(err).ShouldNot(HaveOccurred())
			} else {
				Expect(err).Should(Equal(expectedError))
			}
		},
			Entry("Delete Service Success", nil, nil, nil),
			Entry("Delete Service Success - service doesn't exist", notFoundErr, nil, nil),
			Entry("Get Service Error", errors.New("Test error - Get Service"), nil, errors.New("Test error - Get Service")),
			Entry("Delete Service Error", nil, errors.New("Test error - Delete Service"), errors.New("Test error - Delete Service")),
		)

		AfterAll(func() {
			ctrl.Finish()
		})

	})

})
