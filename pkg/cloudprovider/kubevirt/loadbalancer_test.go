package kubevirt

// import (
// 	"context"
// 	"fmt"
// 	"testing"
// 	"time"

// 	"github.com/golang/mock/gomock"
// 	corev1 "k8s.io/api/core/v1"
// 	"k8s.io/apimachinery/pkg/api/errors"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	"k8s.io/apimachinery/pkg/runtime"
// 	"k8s.io/apimachinery/pkg/runtime/schema"
// 	"k8s.io/apimachinery/pkg/types"
// 	"k8s.io/apimachinery/pkg/util/intstr"
// 	"k8s.io/client-go/kubernetes/fake"
// 	k8stesting "k8s.io/client-go/testing"
// 	cloudprovider "k8s.io/cloud-provider"
// 	kubevirtv1 "kubevirt.io/client-go/api/v1"
// 	"kubevirt.io/client-go/kubecli"
// )

// func mockLoadBalancer(t *testing.T, namespace string) (cloudprovider.LoadBalancer, kubecli.KubevirtClient) {
// 	ctrl := gomock.NewController(t)

// 	serviceList := corev1.ServiceList{
// 		TypeMeta: metav1.TypeMeta{
// 			Kind:       "bla",
// 			APIVersion: "blubb",
// 		},
// 		Items: []corev1.Service{
// 			corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "a6fa1a2662ad011e9b210d663bd873d9",
// 					Namespace: namespace,
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:       "port1",
// 							Protocol:   corev1.ProtocolTCP,
// 							Port:       80,
// 							TargetPort: intstr.FromInt(30005),
// 						},
// 					},
// 				},
// 				Status: corev1.ServiceStatus{},
// 			},
// 			corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "a6fa1a50e2ad011e9b210d663bd873d9",
// 					Namespace: namespace,
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:       "port1",
// 							Protocol:   corev1.ProtocolTCP,
// 							Port:       80,
// 							TargetPort: intstr.FromInt(30005),
// 						},
// 						corev1.ServicePort{
// 							Name:       "port2",
// 							Protocol:   corev1.ProtocolTCP,
// 							Port:       443,
// 							TargetPort: intstr.FromInt(30006),
// 						},
// 					},
// 					Selector: map[string]string{
// 						"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
// 					},
// 				},
// 				Status: corev1.ServiceStatus{
// 					LoadBalancer: corev1.LoadBalancerStatus{
// 						Ingress: []corev1.LoadBalancerIngress{
// 							corev1.LoadBalancerIngress{
// 								IP: "192.168.0.35",
// 							},
// 						},
// 					},
// 				},
// 			},
// 			corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "a6fa1a6582ad011e9b210d663bd873d9",
// 					Namespace: namespace,
// 				},
// 				Status: corev1.ServiceStatus{
// 					LoadBalancer: corev1.LoadBalancerStatus{
// 						Ingress: []corev1.LoadBalancerIngress{
// 							corev1.LoadBalancerIngress{
// 								IP:       "192.168.0.36",
// 								Hostname: "lb1.example.com",
// 							},
// 							corev1.LoadBalancerIngress{
// 								Hostname: "lb2.example.com",
// 							},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}
// 	vmiList := kubevirtv1.VirtualMachineInstanceList{
// 		Items: []kubevirtv1.VirtualMachineInstance{
// 			kubevirtv1.VirtualMachineInstance{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "node1",
// 					Namespace: namespace,
// 					UID:       types.UID("3546a480-2ec3-11e9-b210-d663bd873d93"),
// 					Labels: map[string]string{
// 						"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
// 					},
// 				},
// 			},
// 			kubevirtv1.VirtualMachineInstance{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "node2",
// 					Namespace: namespace,
// 					UID:       types.UID("3546a354-2ec3-11e9-b210-d663bd873d93"),
// 					Labels: map[string]string{
// 						"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
// 						"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
// 					},
// 				},
// 			},
// 			kubevirtv1.VirtualMachineInstance{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "node3",
// 					Namespace: namespace,
// 					UID:       types.UID("3546a228-2ec3-11e9-b210-d663bd873d93"),
// 					Labels:    make(map[string]string, 0),
// 				},
// 			},
// 			kubevirtv1.VirtualMachineInstance{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "node4",
// 					Namespace: namespace,
// 					UID:       types.UID("35469f8a-2ec3-11e9-b210-d663bd873d93"),
// 					Labels: map[string]string{
// 						"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
// 					},
// 				},
// 			},
// 		},
// 	}
// 	podList := corev1.PodList{
// 		Items: []corev1.Pod{
// 			corev1.Pod{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "virt-launcher-node1-test",
// 					Namespace: namespace,
// 					Labels: map[string]string{
// 						"kubevirt.io/created-by":                             "3546a480-2ec3-11e9-b210-d663bd873d93",
// 						"cloud.kubevirt.io/a6fa1a2662ad011e9b210d663bd873d9": "service4",
// 					},
// 				},
// 			},
// 			corev1.Pod{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "virt-launcher-node2-test",
// 					Namespace: namespace,
// 					Labels: map[string]string{
// 						"kubevirt.io/created-by":                             "3546a354-2ec3-11e9-b210-d663bd873d93",
// 						"cloud.kubevirt.io/a6fa1a2662ad011e9b210d663bd873d9": "service4",
// 						"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
// 					},
// 				},
// 			},
// 			corev1.Pod{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "virt-launcher-node3-test",
// 					Namespace: namespace,
// 					Labels: map[string]string{
// 						"kubevirt.io/created-by": "3546a228-2ec3-11e9-b210-d663bd873d93",
// 					},
// 				},
// 			},
// 			corev1.Pod{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      "virt-launcher-node4-test",
// 					Namespace: namespace,
// 					Labels: map[string]string{
// 						"kubevirt.io/created-by":                             "35469f8a-2ec3-11e9-b210-d663bd873d93",
// 						"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
// 					},
// 				},
// 			},
// 		},
// 	}

// 	kubevirt := kubecli.NewMockKubevirtClient(ctrl)

// 	kubernetes := fake.NewSimpleClientset(&serviceList, &podList)
// 	kubernetes.Fake.PrependReactor("create", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
// 		create, ok := action.(k8stesting.CreateAction)
// 		if !ok {
// 			t.Errorf("Called CreateReactor with invalid action: verb '%s', resource: '%s'", action.GetVerb(), action.GetResource())
// 		}
// 		svc, ok := create.GetObject().(*corev1.Service)
// 		if !ok {
// 			t.Errorf("Called CreateReactor with invalid action: verb '%s', resource: '%s'", action.GetVerb(), action.GetResource())
// 		}

// 		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
// 			go func() {
// 				time.Sleep(3 * time.Second)
// 				switch svc.ObjectMeta.Name {
// 				case "af6ebf1722bb111e9b210d663bd873d9":
// 					svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.0.37"}}
// 				case "a354699682ec311e9b210d663bd873d9":
// 					svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.0.38"}}
// 				case "a35469a9e2ec311e9b210d663bd873d9":
// 					svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.168.0.39"}}
// 				}

// 				kubernetes.CoreV1().Services(namespace).UpdateStatus(svc)
// 			}()
// 		}
// 		return false, nil, nil
// 	})
// 	kubevirt.EXPECT().CoreV1().Return(kubernetes.CoreV1()).AnyTimes()

// 	vmiInterface := kubecli.NewMockVirtualMachineInstanceInterface(ctrl)
// 	kubevirt.EXPECT().VirtualMachineInstance(gomock.Eq(namespace)).Return(vmiInterface).AnyTimes()
// 	//vmiInterface.EXPECT().List(gomock.Eq(&metav1.ListOptions{})).Return(&vmiList, nil).AnyTimes()
// 	vmiInterface.EXPECT().List(gomock.Any()).DoAndReturn(func(opts *metav1.ListOptions) (*kubevirtv1.VirtualMachineInstanceList, error) {
// 		if opts.LabelSelector != "" {
// 			vmis := make([]kubevirtv1.VirtualMachineInstance, 0, len(vmiList.Items))
// 			for _, vmi := range vmiList.Items {
// 				for k, v := range vmi.ObjectMeta.Labels {
// 					if fmt.Sprintf("%s=%s", k, v) == opts.LabelSelector {
// 						vmis = append(vmis, vmi)
// 						break
// 					}
// 				}
// 			}
// 			return &kubevirtv1.VirtualMachineInstanceList{
// 				Items: vmis,
// 			}, nil
// 		}
// 		return &vmiList, nil
// 	}).AnyTimes()
// 	vmiInterface.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(name string, options *metav1.GetOptions) (*kubevirtv1.VirtualMachineInstance, error) {
// 		for _, vmi := range vmiList.Items {
// 			if vmi.ObjectMeta.Name == name {
// 				return &vmi, nil
// 			}
// 		}
// 		err := errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, name)
// 		return nil, err
// 	}).AnyTimes()
// 	vmiInterface.EXPECT().Update(gomock.Any()).AnyTimes()

// 	return &loadbalancer{
// 		namespace: namespace,
// 		kubevirt:  kubevirt,
// 	}, kubevirt
// }

// func makeLoadBalancerStatus(ips, hostnames []string) *corev1.LoadBalancerStatus {
// 	status := &corev1.LoadBalancerStatus{}
// 	lenIps := len(ips)
// 	lenHostnames := len(hostnames)
// 	var length int
// 	if lenIps > lenHostnames {
// 		length = lenIps
// 	} else {
// 		length = lenHostnames
// 	}
// 	if length == 0 {
// 		return status
// 	}
// 	ingressList := make([]corev1.LoadBalancerIngress, length)

// 	for i := 0; i < length; i++ {
// 		var ip, hostname string
// 		if i < lenIps {
// 			ip = ips[i]
// 		}
// 		if i < lenHostnames {
// 			hostname = hostnames[i]
// 		}
// 		ingressList[i] = corev1.LoadBalancerIngress{
// 			IP:       ip,
// 			Hostname: hostname,
// 		}
// 	}
// 	status.Ingress = ingressList
// 	return status
// }

// func cmpLoadBalancerStatuses(a, b *corev1.LoadBalancerStatus) bool {
// 	if a == b {
// 		return true
// 	}

// 	if (a == nil || b == nil) || (len(a.Ingress) != len(b.Ingress)) {
// 		return false
// 	}

// 	for i, aIngress := range a.Ingress {
// 		if (aIngress.IP != b.Ingress[i].IP) || (aIngress.Hostname != b.Ingress[i].Hostname) {
// 			return false
// 		}
// 	}

// 	return true
// }

// func TestGetLoadBalancer(t *testing.T) {
// 	lb, _ := mockLoadBalancer(t, "testGetLoadBalancerName")
// 	tests := []struct {
// 		serviceUID       types.UID
// 		clusterName      string
// 		expectedLBStatus *corev1.LoadBalancerStatus
// 		expectedExists   bool
// 		expectedError    error
// 	}{
// 		{types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", makeLoadBalancerStatus([]string{}, []string{}), true, nil},
// 		{types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", makeLoadBalancerStatus([]string{"192.168.0.35"}, []string{}), true, nil},
// 		{types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", makeLoadBalancerStatus([]string{"192.168.0.36"}, []string{"lb1.example.com", "lb2.example.com"}), true, nil},
// 		{types.UID("does-not-exist-in-k8s"), "cluster2", nil, false, nil},
// 	}

// 	for _, test := range tests {
// 		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: test.serviceUID}}
// 		status, exists, err := lb.GetLoadBalancer(context.TODO(), test.clusterName, svc)
// 		if !cmpLoadBalancerStatuses(status, test.expectedLBStatus) {
// 			t.Errorf("Expected: %v, got: %v", test.expectedLBStatus, status)
// 		}
// 		if test.expectedExists != exists {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedExists, exists)
// 		}
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 	}
// }

// func TestGetLoadBalancerName(t *testing.T) {
// 	lb, _ := mockLoadBalancer(t, "testGetLoadBalancerName")
// 	tests := []struct {
// 		serviceUID   types.UID
// 		clusterName  string
// 		expectedName string
// 	}{
// 		{types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", "a6fa1a2662ad011e9b210d663bd873d9"},
// 		{types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", "a6fa1a50e2ad011e9b210d663bd873d9"},
// 		{types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", "a6fa1a6582ad011e9b210d663bd873d9"},
// 	}

// 	for _, test := range tests {
// 		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: test.serviceUID}}
// 		name := lb.GetLoadBalancerName(context.TODO(), test.clusterName, svc)
// 		if name != test.expectedName {
// 			t.Errorf("Expected: %v, got: %v", test.expectedName, name)
// 		}
// 	}
// }

// func TestEnsureLoadBalancer(t *testing.T) {
// 	namespace := "testEnsureLoadBalancer"
// 	lb, kubevirt := mockLoadBalancer(t, namespace)
// 	tests := []struct {
// 		service                *corev1.Service
// 		nodes                  []*corev1.Node
// 		clusterName            string
// 		expectedServiceName    string
// 		expectedLoadBalancerIP string
// 		expectedTargetVmiNames []string
// 		expectedError          error
// 	}{
// 		// Testcase: No LB exists for service, no nodes given
// 		// Expected: LB Service will be created, no VMIs & pods labelled, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service1",
// 					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30001,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{},
// 			"",
// 			"af6ebf1722bb111e9b210d663bd873d9",
// 			"192.168.0.37",
// 			make([]string, 0),
// 			nil,
// 		},
// 		// Testcase: No LB exists for service, single node given
// 		// Expected: LB Service will be created, one VMI & pod labelled, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service2",
// 					UID:  types.UID("35469968-2ec3-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30002,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 			},
// 			"cluster1",
// 			"a354699682ec311e9b210d663bd873d9",
// 			"192.168.0.38",
// 			[]string{"node1"},
// 			nil,
// 		},
// 		// Testcase: No LB exists for service, three nodes given
// 		// Expected: LB Service will be created, three VMIs & pods labelled, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service3",
// 					UID:  types.UID("35469a9e-2ec3-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30003,
// 						},
// 						corev1.ServicePort{
// 							Name:     "port2",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     443,
// 							NodePort: 30004,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node2",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node2",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node3",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node3",
// 					},
// 				},
// 			},
// 			"cluster1",
// 			"a35469a9e2ec311e9b210d663bd873d9",
// 			"192.168.0.39",
// 			[]string{"node1", "node2", "node3"},
// 			nil,
// 		},
// 		// Testcase: LB already exists for service, unchanged amount of nodes
// 		// Expected: LB Service unchanged, labels on VMIs & pods unchanged, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30005,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node2",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node2",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			"a6fa1a50e2ad011e9b210d663bd873d9",
// 			"192.168.0.35",
// 			[]string{"node1", "node2"},
// 			nil,
// 		},
// 		// Testcase: LB already exists for service, new nodes given
// 		// Expected: LB Service unchanged, new VMIs & pods labelled, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30005,
// 						},
// 						corev1.ServicePort{
// 							Name:     "port2",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     443,
// 							NodePort: 30006,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node2",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node2",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node3",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node3",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			"a6fa1a50e2ad011e9b210d663bd873d9",
// 			"192.168.0.35",
// 			[]string{"node1", "node2"},
// 			nil,
// 		},
// 		// Testcase: LB already exists for service, nodes removed
// 		// Expected: LB Service unchanged, labels removed from VMIs & pods, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			"a6fa1a50e2ad011e9b210d663bd873d9",
// 			"192.168.0.35",
// 			[]string{"node1"},
// 			nil,
// 		},
// 	}

// 	for _, test := range tests {
// 		_, err := lb.EnsureLoadBalancer(context.TODO(), test.clusterName, test.service, test.nodes)
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 		svc, _ := kubevirt.CoreV1().Services(namespace).Get(test.expectedServiceName, metav1.GetOptions{})
// 		if svc.ObjectMeta.Name != test.expectedServiceName {
// 			t.Errorf("Expected: %v, got: %v", test.expectedServiceName, svc.ObjectMeta.Name)
// 		}
// 		if svc.Status.LoadBalancer.Ingress[0].IP != test.expectedLoadBalancerIP {
// 			t.Errorf("Expected: %v, got: %v", test.expectedLoadBalancerIP, svc.Status.LoadBalancer.Ingress[0].IP)
// 		}
// 		labelKey := fmt.Sprintf("cloud.kubevirt.io/%s", test.expectedServiceName)
// 		if labelValue, ok := svc.Spec.Selector[labelKey]; ok {
// 			if labelValue != test.service.ObjectMeta.Name {
// 				t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 			}
// 		} else {
// 			t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 		}
// 		if len(test.service.Spec.Ports) != len(svc.Spec.Ports) {
// 			t.Errorf("Expected: '%d ports', got '%d ports'", len(test.service.Spec.Ports), len(svc.Spec.Ports))
// 		}
// 		for i, port := range test.service.Spec.Ports {
// 			lbPort := svc.Spec.Ports[i]
// 			if port.Name != lbPort.Name {
// 				t.Errorf("Expected: '%v', got '%v'", port.Name, lbPort.Name)
// 			}
// 			if port.Protocol != lbPort.Protocol {
// 				t.Errorf("Expected: '%v', got '%v'", port.Protocol, lbPort.Protocol)
// 			}
// 			if port.Port != lbPort.Port {
// 				t.Errorf("Expected: '%v', got '%v'", port.Port, lbPort.Port)
// 			}
// 			nodePort := intstr.IntOrString{Type: intstr.Int, IntVal: port.NodePort}
// 			if nodePort != lbPort.TargetPort {
// 				t.Errorf("Expected: '%v', got '%v'", port.NodePort, lbPort.TargetPort)
// 			}
// 		}
// 		allVMIList, _ := kubevirt.VirtualMachineInstance(namespace).List(&metav1.ListOptions{})
// 		vmiMap := make(map[string]kubevirtv1.VirtualMachineInstance, len(allVMIList.Items))
// 		for _, vmi := range allVMIList.Items {
// 			vmiMap[vmi.ObjectMeta.Name] = vmi
// 		}
// 		allPodList, _ := kubevirt.CoreV1().Pods(namespace).List(metav1.ListOptions{})
// 		podMap := make(map[string]corev1.Pod, len(allPodList.Items))
// 		for _, pod := range allPodList.Items {
// 			podMap[pod.ObjectMeta.Name] = pod
// 		}
// 		for _, node := range test.nodes {
// 			nodeName := node.ObjectMeta.Name
// 			instanceID, _ := instanceIDFromProviderID(node.Spec.ProviderID)
// 			podName := fmt.Sprintf("virt-launcher-%s-test", instanceID)
// 			vmi, ok := vmiMap[instanceID]
// 			if !ok {
// 				t.Errorf("Expected: matching VMI for node '%s', got: VMI not found", nodeName)
// 			}
// 			if labelValue, ok := vmi.ObjectMeta.Labels[labelKey]; ok {
// 				if labelValue != test.service.ObjectMeta.Name {
// 					t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 				}
// 			} else {
// 				t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 			}
// 			pod, ok := podMap[podName]
// 			if !ok {
// 				t.Errorf("Expected: matching pod for node '%s', got: pod not found", nodeName)
// 			}
// 			if labelValue, ok := pod.ObjectMeta.Labels[labelKey]; ok {
// 				if labelValue != test.service.ObjectMeta.Name {
// 					t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 				}
// 			} else {
// 				t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 			}
// 			// Remove labeled resources from vmi and pod maps
// 			delete(vmiMap, instanceID)
// 			delete(podMap, podName)
// 		}
// 		// At this point all remaining resources in vmi and pod maps should be unlabeled
// 		for _, vmi := range vmiMap {
// 			if labelValue, ok := vmi.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 		for _, pod := range podMap {
// 			if labelValue, ok := pod.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 	}
// }

// func TestUpdateLoadBalancer(t *testing.T) {
// 	namespace := "testUpdateLoadBalancer"
// 	lb, kubevirt := mockLoadBalancer(t, namespace)
// 	tests := []struct {
// 		service                *corev1.Service
// 		nodes                  []*corev1.Node
// 		clusterName            string
// 		expectedTargetVmiNames []string
// 		expectedError          error
// 	}{
// 		// Testcase: unchanged amount of nodes
// 		// Expected: labels on VMIs & pods unchanged, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30005,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node2",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node2",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			[]string{"node1", "node2"},
// 			nil,
// 		},
// 		// Testcase: new nodes given
// 		// Expected: new VMIs & pods labelled, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30005,
// 						},
// 						corev1.ServicePort{
// 							Name:     "port2",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     443,
// 							NodePort: 30006,
// 						},
// 					},
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node2",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node2",
// 					},
// 				},
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node3",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node3",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			[]string{"node1", "node2", "node3"},
// 			nil,
// 		},
// 		// Testcase: nodes removed
// 		// Expected: labels removed from VMIs & pods, no error
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 				},
// 			},
// 			[]*corev1.Node{
// 				&corev1.Node{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "node1",
// 					},
// 					Spec: corev1.NodeSpec{
// 						ProviderID: "kubevirt://node1",
// 					},
// 				},
// 			},
// 			"cluster2",
// 			[]string{"node1"},
// 			nil,
// 		},
// 	}

// 	for _, test := range tests {
// 		err := lb.UpdateLoadBalancer(context.TODO(), test.clusterName, test.service, test.nodes)
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 		serviceName := lb.GetLoadBalancerName(context.TODO(), test.clusterName, test.service)
// 		svc, _ := kubevirt.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
// 		labelKey := fmt.Sprintf("cloud.kubevirt.io/%s", serviceName)
// 		if labelValue, ok := svc.Spec.Selector[labelKey]; ok {
// 			if labelValue != test.service.ObjectMeta.Name {
// 				t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 			}
// 		} else {
// 			t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 		}
// 		allVMIList, _ := kubevirt.VirtualMachineInstance(namespace).List(&metav1.ListOptions{})
// 		vmiMap := make(map[string]kubevirtv1.VirtualMachineInstance, len(allVMIList.Items))
// 		for _, vmi := range allVMIList.Items {
// 			vmiMap[vmi.ObjectMeta.Name] = vmi
// 		}
// 		allPodList, _ := kubevirt.CoreV1().Pods(namespace).List(metav1.ListOptions{})
// 		podMap := make(map[string]corev1.Pod, len(allPodList.Items))
// 		for _, pod := range allPodList.Items {
// 			podMap[pod.ObjectMeta.Name] = pod
// 		}
// 		for _, node := range test.nodes {
// 			nodeName := node.ObjectMeta.Name
// 			instanceID, _ := instanceIDFromProviderID(node.Spec.ProviderID)
// 			podName := fmt.Sprintf("virt-launcher-%s-test", instanceID)
// 			vmi, ok := vmiMap[instanceID]
// 			if !ok {
// 				t.Errorf("Expected: matching VMI for node '%s', got: VMI not found", nodeName)
// 			}
// 			if labelValue, ok := vmi.ObjectMeta.Labels[labelKey]; ok {
// 				if labelValue != test.service.ObjectMeta.Name {
// 					t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 				}
// 			} else {
// 				t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 			}
// 			pod, ok := podMap[podName]
// 			if !ok {
// 				t.Errorf("Expected: matching pod for node '%s', got: pod not found", nodeName)
// 			}
// 			if labelValue, ok := pod.ObjectMeta.Labels[labelKey]; ok {
// 				if labelValue != test.service.ObjectMeta.Name {
// 					t.Errorf("Expected: %s=%v, got: %s=%v", labelKey, test.service.ObjectMeta.Name, labelKey, labelValue)
// 				}
// 			} else {
// 				t.Errorf("Expected: %s=%v, got: label not set", labelKey, test.service.ObjectMeta.Name)
// 			}
// 			// Remove labeled resources from vmi and pod maps
// 			delete(vmiMap, instanceID)
// 			delete(podMap, podName)
// 		}
// 		// At this point all remaining resources in vmi and pod maps should be unlabeled
// 		for _, vmi := range vmiMap {
// 			if labelValue, ok := vmi.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 		for _, pod := range podMap {
// 			if labelValue, ok := pod.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 	}
// }

// func TestEnsureLoadBalancerDeleted(t *testing.T) {
// 	namespace := "testEnsureLoadBalancerDeleted"
// 	lb, kubevirt := mockLoadBalancer(t, namespace)
// 	tests := []struct {
// 		service     *corev1.Service
// 		clusterName string
// 	}{
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service1",
// 					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30001,
// 						},
// 					},
// 				},
// 			},
// 			"cluster1",
// 		},
// 		{
// 			&corev1.Service{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "service4",
// 					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
// 				},
// 				Spec: corev1.ServiceSpec{
// 					Type: corev1.ServiceTypeLoadBalancer,
// 					Ports: []corev1.ServicePort{
// 						corev1.ServicePort{
// 							Name:     "port1",
// 							Protocol: corev1.ProtocolTCP,
// 							Port:     80,
// 							NodePort: 30005,
// 						},
// 					},
// 				},
// 			},
// 			"cluster2",
// 		},
// 	}

// 	for _, test := range tests {
// 		err := lb.EnsureLoadBalancerDeleted(context.TODO(), test.clusterName, test.service)
// 		serviceName := lb.GetLoadBalancerName(context.TODO(), test.clusterName, test.service)
// 		_, err = kubevirt.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
// 		if !errors.IsNotFound(err) {
// 			if err != nil {
// 				t.Errorf("Expected: service %s to be deleted, got: %v", test.service.ObjectMeta.Name, err)
// 			} else {
// 				t.Errorf("Expected: service %s to be deleted, got: service still exists", test.service.ObjectMeta.Name)
// 			}
// 		}
// 		labelKey := fmt.Sprintf("cloud.kubevirt.io/%s", serviceName)
// 		vmis, _ := kubevirt.VirtualMachineInstance(namespace).List(&metav1.ListOptions{})
// 		pods, _ := kubevirt.CoreV1().Pods(namespace).List(metav1.ListOptions{})
// 		for _, vmi := range vmis.Items {
// 			if labelValue, ok := vmi.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 		for _, pod := range pods.Items {
// 			if labelValue, ok := pod.ObjectMeta.Labels[labelKey]; ok {
// 				t.Errorf("Expected: label '%s' not set, got: %s=%v", labelKey, labelKey, labelValue)
// 			}
// 		}
// 	}
// }
