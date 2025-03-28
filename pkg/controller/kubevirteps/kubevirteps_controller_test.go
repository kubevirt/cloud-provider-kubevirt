//nolint:unparam
package kubevirteps

import (
	"context"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/component-base/metrics/prometheus/controllers"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/cloud-provider-kubevirt/pkg/provider"
)

const (
	tenantNamespace = "tenant-namespace"
	infraNamespace  = "test"
)

type testKubevirtEPSController struct {
	controller   *Controller
	tenantClient *fake.Clientset
	infraClient  *fake.Clientset
	infraDynamic *dfake.FakeDynamicClient
}

func createInfraServiceLB(name, tenantServiceName, clusterName string, servicePort v1.ServicePort, externalTrafficPolicy v1.ServiceExternalTrafficPolicy) *v1.Service {
	var selector map[string]string
	if externalTrafficPolicy == v1.ServiceExternalTrafficPolicyCluster {
		selector = map[string]string{
			"cluster.x-k8s.io/role":         "worker",
			"cluster.x-k8s.io/cluster-name": clusterName,
		}
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: infraNamespace,
			Labels: map[string]string{
				kubevirt.TenantServiceNameLabelKey:      tenantServiceName,
				kubevirt.TenantServiceNamespaceLabelKey: tenantNamespace,
				kubevirt.TenantClusterNameLabelKey:      clusterName,
			},
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				servicePort,
			},
			Type:                  v1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: externalTrafficPolicy,
			Selector:              selector,
			IPFamilies: []v1.IPFamily{
				v1.IPv4Protocol,
			},
		},
	}
}

func createUnstructuredVMINode(name, nodeName, ip string) *unstructured.Unstructured {
	vmi := &unstructured.Unstructured{}
	vmi.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": "kubevirt.io/v1",
		"kind":       "VirtualMachineInstance",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": infraNamespace,
		},
		"status": map[string]interface{}{
			"phase":    "Running",
			"nodeName": nodeName,
			"interfaces": []interface{}{
				map[string]interface{}{
					"name":      "default",
					"ipAddress": ip,
				},
			},
		},
	})
	return vmi
}

func createPort(name string, port int32, protocol v1.Protocol) *discoveryv1.EndpointPort {
	return &discoveryv1.EndpointPort{
		Name:     &name,
		Port:     &port,
		Protocol: &protocol,
	}
}

func createEndpoint(ip, nodeName string, ready, serving, terminating bool) *discoveryv1.Endpoint {
	return &discoveryv1.Endpoint{
		Addresses: []string{ip},
		Conditions: discoveryv1.EndpointConditions{
			Ready:       &ready,
			Serving:     &serving,
			Terminating: &terminating,
		},
		NodeName: &nodeName,
	}
}

func createTenantEPSlice(
	name, labelServiceName string, addressType discoveryv1.AddressType,
	port discoveryv1.EndpointPort, endpoints []discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tenantNamespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: labelServiceName,
			},
		},
		AddressType: addressType,
		Ports: []discoveryv1.EndpointPort{
			port,
		},
		Endpoints: endpoints,
	}
}

func createAndAssertVMI(node, nodeName, ip string) {
	vmi := createUnstructuredVMINode(node, nodeName, ip)
	_, err := testVals.infraDynamic.Resource(kubevirtv1.VirtualMachineInstanceGroupVersionKind.GroupVersion().WithResource("virtualmachineinstances")).
		Namespace(infraNamespace).Create(context.TODO(), vmi, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	Eventually(func() (bool, error) {
		vmiList, err := testVals.infraDynamic.Resource(kubevirtv1.VirtualMachineInstanceGroupVersionKind.GroupVersion().WithResource("virtualmachineinstances")).
			Namespace(infraNamespace).Get(context.TODO(), node, metav1.GetOptions{})
		if err == nil || vmiList != nil {
			return true, err
		}
		return false, err
	}).Should(BeTrue(), "VMI in infra cluster should be created")
}

func createAndAssertTenantSlice(name, labelServiceName string, addressType discoveryv1.AddressType, port discoveryv1.EndpointPort, endpoints []discoveryv1.Endpoint) {
	epSlice := createTenantEPSlice(name, labelServiceName, addressType, port, endpoints)
	_, _ = testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).Create(context.TODO(), epSlice, metav1.CreateOptions{})
	// Check if tenant Endpointslice is created
	Eventually(func() (bool, error) {
		eps, err := testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err == nil || eps != nil {
			return true, err
		}
		return false, err
	}).Should(BeTrue(), "EndpointSlice in tenant cluster should be created")
}

func createAndAssertInfraServiceLB(name, tenantServiceName, clusterName string, servicePort v1.ServicePort, externalTrafficPolicy v1.ServiceExternalTrafficPolicy) {
	svc := createInfraServiceLB(name, tenantServiceName, clusterName, servicePort, externalTrafficPolicy)
	_, _ = testVals.infraClient.CoreV1().Services(infraNamespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	// Check if the service is created
	Eventually(func() (bool, error) {
		svc, err := testVals.infraClient.CoreV1().Services(infraNamespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err == nil || svc != nil {
			return true, err
		}
		return false, err

	}).Should(BeTrue(), "")
}

func setupTestKubevirtEPSController() *testKubevirtEPSController {
	var tenantClient *fake.Clientset
	var infraClient *fake.Clientset

	tenantClient = fake.NewSimpleClientset()
	infraClient = fake.NewSimpleClientset()

	s := runtime.NewScheme()
	infraDynamic := dfake.NewSimpleDynamicClientWithCustomListKinds(s, map[schema.GroupVersionResource]string{
		{
			Group:    kubevirtv1.GroupVersion.Group,
			Version:  kubevirtv1.GroupVersion.Version,
			Resource: "virtualmachineinstances",
		}: "VirtualMachineInstanceList",
	})

	controller := NewKubevirtEPSController(tenantClient, infraClient, infraDynamic, "test")

	err := controller.Init()
	if err != nil {
		klog.Errorf("Failed to initialize kubevirtEPSController: %v", err)
		klog.Fatal(err)
	}

	return &testKubevirtEPSController{
		controller:   controller,
		tenantClient: tenantClient,
		infraClient:  infraClient,
		infraDynamic: infraDynamic,
	}
}

func (testVals *testKubevirtEPSController) runKubevirtEPSController(ctx context.Context) {
	metrics := controllers.NewControllerManagerMetrics("test")
	go testVals.controller.Run(1, ctx.Done(), metrics)
}

var _ = g.Describe("KubevirtEPSController start", g.Ordered, func() {
	g.Context("With starting the controller", g.Ordered, func() {

		g.It("Should start the controller", func() {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			testVals = setupTestKubevirtEPSController()
			testVals.runKubevirtEPSController(ctx)
		})
	})
})

var (
	stop     context.CancelFunc
	ctx      context.Context
	testVals *testKubevirtEPSController
)

var _ = g.Describe("KubevirtEPSController", g.Ordered, func() {

	g.Context("With starting the controller", g.Ordered, func() {
		g.It("Should start the controller", func() {
			ctx, stop = context.WithCancel(context.Background())
			defer stop()
			testVals = setupTestKubevirtEPSController()
			testVals.runKubevirtEPSController(ctx)

			cache.WaitForCacheSync(ctx.Done(),
				testVals.controller.tenantFactory.Discovery().V1().EndpointSlices().Informer().HasSynced,
				testVals.controller.infraFactory.Core().V1().Services().Informer().HasSynced)
		})
	})

	g.Context("With adding an infraService", g.Ordered, func() {
		// Startup and wait for cache sync
		g.BeforeEach(func() {
			ctx, stop = context.WithCancel(context.Background())
			testVals = setupTestKubevirtEPSController()
			testVals.runKubevirtEPSController(ctx)

			cache.WaitForCacheSync(ctx.Done(),
				testVals.controller.tenantFactory.Discovery().V1().EndpointSlices().Informer().HasSynced,
				testVals.controller.infraFactory.Core().V1().Services().Informer().HasSynced)

		})

		// Stop the controller
		g.AfterEach(func() {
			stop()
		})

		g.It("Should reconcile a new Endpointslice on the infra cluster", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error
			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			Expect(*epsList.Items[0].Endpoints[0].NodeName).To(Equal("ip-10-32-5-13"))
		})

		g.It("Should update the Endpointslice when a tenant Endpointslice is updated", func() {

			ipAddr1 := "123.45.67.11"
			ipAddr2 := "123.99.99.99"
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", ipAddr1)
			createAndAssertVMI("worker-1-test", "ip-10-32-5-15", ipAddr2)

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint(ipAddr1, "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err := testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 &&
					len(epsList.Items[0].Endpoints) == 1 &&
					*epsList.Items[0].Endpoints[0].NodeName == "ip-10-32-5-13" {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			// Update the tenant Endpointslice
			epSlice := createTenantEPSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{
					*createEndpoint(ipAddr1, "worker-0-test", true, true, false),
					*createEndpoint(ipAddr2, "worker-1-test", true, true, false),
				})
			_, err := testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).Update(context.TODO(), epSlice, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			// Check if tenant Endpointslice is updated
			Eventually(func() (bool, error) {
				epsList, err := testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 && len(epsList.Items[0].Endpoints) == 2 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in tenant cluster should be updated")

			// Check if the controller updates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err := testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 && len(epsList.Items[0].Endpoints) == 2 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be updated by the controller reconciler")
		})

		g.It("Should update the Endpointslice when the infra Service external traffic policy changes.", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error
			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			Expect(*epsList.Items[0].Endpoints[0].NodeName).To(Equal("ip-10-32-5-13"))

			// Update the service's external traffic policy to Cluster
			svc := createInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyCluster)

			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 0 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be deleted by the controller reconciler")

			// Update the service's external traffic policy to Local
			svc = createInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")
		})

		g.It("Should update the Endpointslice when the infra Service labels are updated.", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error
			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			Expect(*epsList.Items[0].Endpoints[0].NodeName).To(Equal("ip-10-32-5-13"))

			// Update the service's labels
			svc := createInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)
			svc.Labels["test-label"] = "test-value"
			svc.Labels["test-label-2"] = "test-value-2"

			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					if epsList.Items[0].Labels["test-label"] == "test-value" && epsList.Items[0].Labels["test-label-2"] == "test-value-2" {
						return true, err
					}
					return false, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should have the two added labels")

			// Update the service's external traffic policy to Cluster
			svc = createInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)
			svc.Labels["test-label"] = "test-value"

			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					if epsList.Items[0].Labels["test-label"] == "test-value" && epsList.Items[0].Labels["test-label-2"] == "test-value-2" {
						return true, err
					}
					return false, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster still has the two added labels")
		})

		g.It("Should update the Endpointslice when the infra Service port is updated.", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error
			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					if *epsList.Items[0].Ports[0].Port == 30390 {
						return true, err
					}
					return false, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			Expect(*epsList.Items[0].Endpoints[0].NodeName).To(Equal("ip-10-32-5-13"))

			// Update the service's port
			svc := createInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30440}},
				v1.ServiceExternalTrafficPolicyLocal)

			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					if *epsList.Items[0].Ports[0].Port == 30440 {
						return true, err
					}
					return false, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should have the two added labels")
		})

		g.It("Should delete the Endpointslice when the Service in infra is deleted", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error
			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					if *epsList.Items[0].Ports[0].Port == 30390 {
						return true, err
					}
					return false, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			Expect(*epsList.Items[0].Endpoints[0].NodeName).To(Equal("ip-10-32-5-13"))

			// Delete the service
			err = testVals.infraClient.CoreV1().Services(infraNamespace).Delete(context.TODO(), "infra-service-name", metav1.DeleteOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 0 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be deleted.")
		})

		g.It("Should not update the Endpointslice on the infra cluster because VMI is not present", func() {
			// Create VMI in infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create Endpoinslices in tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create service in infra cluster
			createAndAssertInfraServiceLB("infra-service-name", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}}, v1.ServiceExternalTrafficPolicyLocal)

			// Check if the controller creates the EndpointSlice in the infra cluster
			Eventually(func() (bool, error) {
				epsList, err := testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be created by the controller reconciler")

			//
			epSlice := createTenantEPSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{
					*createEndpoint("123.45.67.89", "worker-0-test", true, true, false),
					*createEndpoint("112.34.56.78", "worker-1-test", true, true, false),
				})

			_, err := testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).Update(context.TODO(), epSlice, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			// Check if tenant Endpointslice is updated
			Eventually(func() (bool, error) {
				epsList, err := testVals.tenantClient.DiscoveryV1().EndpointSlices(tenantNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 && len(epsList.Items[0].Endpoints) == 2 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "EndpointSlice in tenant cluster should be updated")

			//Expect call to the infraDynamic.Get to return the VMI
			Eventually(func() (bool, error) {
				for _, action := range testVals.infraDynamic.Actions() {
					if action.Matches("get", "virtualmachineinstances") &&
						action.GetNamespace() == infraNamespace {
						getAction := action.(testing.GetAction)
						if getAction.GetName() == "worker-1-test" {
							return true, nil
						}
					}
				}
				return false, nil
			}).Should(BeTrue(), "Expect call to the infraDynamic.Get to return the VMI")

		})

		g.It("Should reconcile after infra EndpointSlice deletion and restore it", func() {
			// Create a VMI in the infra cluster
			// This ensures that when tenant EndpointSlice is created, it can be reconciled properly
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create an EndpointSlice in the tenant cluster representing the desired state
			createAndAssertTenantSlice("test-epslice-infra", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Create a Service in the infra cluster that should trigger the creation of an EndpointSlice in the infra cluster
			createAndAssertInfraServiceLB("infra-service-restore", "tenant-service-name", "test-cluster",
				v1.ServicePort{Name: "web", Port: 80, NodePort: 31900, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{IntVal: 30390}},
				v1.ServiceExternalTrafficPolicyLocal)

			var epsList *discoveryv1.EndpointSliceList
			var err error

			// Wait until the infra EndpointSlice is created by the controller
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsList.Items) == 1 {
					return true, err
				} else {
					return false, err
				}
			}).Should(BeTrue(), "Infra EndpointSlice should be created by the controller")

			// Now, simulate an external deletion of the EndpointSlice in the infra cluster
			err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).Delete(context.TODO(), epsList.Items[0].Name, metav1.DeleteOptions{})
			Expect(err).To(BeNil(), "Deleting infra EndpointSlice should succeed")

			// The controller, now watching infra EndpointSlices, should detect the removal
			// and trigger a reconcile to restore it.
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				// After some time, we expect exactly one EndpointSlice to be recreated.
				if err == nil && len(epsList.Items) == 1 {
					return true, nil
				}
				return false, err
			}).Should(BeTrue(), "EndpointSlice in infra cluster should be recreated by the controller after deletion")
		})

		g.It("Should correctly handle multiple unique ports in EndpointSlice", func() {
			// Create a VMI in the infra cluster
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			// Create an EndpointSlice in the tenant cluster
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})

			// Define multiple ports for the Service
			servicePorts := []v1.ServicePort{
				{
					Name:       "client",
					Protocol:   v1.ProtocolTCP,
					Port:       10001,
					TargetPort: intstr.FromInt(30396),
					NodePort:   30396,
				},
				{
					Name:       "dashboard",
					Protocol:   v1.ProtocolTCP,
					Port:       8265,
					TargetPort: intstr.FromInt(31003),
					NodePort:   31003,
				},
				{
					Name:       "metrics",
					Protocol:   v1.ProtocolTCP,
					Port:       8080,
					TargetPort: intstr.FromInt(30452),
					NodePort:   30452,
				},
			}

			createAndAssertInfraServiceLB("infra-multiport-service", "tenant-service-name", "test-cluster",
				servicePorts[0], v1.ServiceExternalTrafficPolicyLocal)

			svc, err := testVals.infraClient.CoreV1().Services(infraNamespace).Get(context.TODO(), "infra-multiport-service", metav1.GetOptions{})
			Expect(err).To(BeNil())

			svc.Spec.Ports = servicePorts
			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			var epsListMultiPort *discoveryv1.EndpointSliceList

			Eventually(func() (bool, error) {
				epsListMultiPort, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).List(context.TODO(), metav1.ListOptions{})
				if len(epsListMultiPort.Items) != 1 {
					return false, err
				}

				createdSlice := epsListMultiPort.Items[0]
				expectedPortNames := []string{"client", "dashboard", "metrics"}
				foundPortNames := []string{}

				for _, port := range createdSlice.Ports {
					if port.Name != nil {
						foundPortNames = append(foundPortNames, *port.Name)
					}
				}

				if len(foundPortNames) != len(expectedPortNames) {
					return false, err
				}

				portSet := sets.NewString(foundPortNames...)
				expectedPortSet := sets.NewString(expectedPortNames...)
				return portSet.Equal(expectedPortSet), err
			}).Should(BeTrue(), "EndpointSlice should contain all unique ports from the Service without duplicates")
		})

		g.It("Should not panic when Service changes to have a non-nil selector, causing EndpointSlice deletion with no new slices to create", func() {
			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")
			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{*createEndpoint("123.45.67.89", "worker-0-test", true, true, false)})
			createAndAssertInfraServiceLB("infra-service-no-selector", "tenant-service-name", "test-cluster",
				v1.ServicePort{
					Name:       "web",
					Port:       80,
					NodePort:   31900,
					Protocol:   v1.ProtocolTCP,
					TargetPort: intstr.IntOrString{IntVal: 30390},
				},
				v1.ServiceExternalTrafficPolicyLocal,
			)

			// Wait for the controller to create an EndpointSlice in the infra cluster.
			var epsList *discoveryv1.EndpointSliceList
			var err error
			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).
					List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false, err
				}
				// Wait exactly 1 slice
				if len(epsList.Items) == 1 {
					return true, nil
				}
				return false, nil
			}).Should(BeTrue(), "Controller should create an EndpointSlice in infra cluster for the LB service")

			svcWithSelector, err := testVals.infraClient.CoreV1().Services(infraNamespace).
				Get(context.TODO(), "infra-service-no-selector", metav1.GetOptions{})
			Expect(err).To(BeNil())

			// Let's set any selector to run the slice deletion logic
			svcWithSelector.Spec.Selector = map[string]string{"test": "selector-added"}
			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).
				Update(context.TODO(), svcWithSelector, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err = testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).
					List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false, err
				}
				// We expect that after the update service.EndpointSlice will become 0
				if len(epsList.Items) == 0 {
					return true, nil
				}
				return false, nil
			}).Should(BeTrue(), "Existing EndpointSlice should be removed because Service now has a selector")
		})

		g.It("Should remove EndpointSlices and not recreate them when a previously no-selector Service obtains a selector", func() {
			testVals.infraClient.Fake.PrependReactor("create", "endpointslices", func(action testing.Action) (bool, runtime.Object, error) {
				createAction := action.(testing.CreateAction)
				slice := createAction.GetObject().(*discoveryv1.EndpointSlice)
				if slice.Name == "" && slice.GenerateName != "" {
					slice.Name = slice.GenerateName + "-fake001"
				}
				return false, slice, nil
			})

			createAndAssertVMI("worker-0-test", "ip-10-32-5-13", "123.45.67.89")

			createAndAssertTenantSlice("test-epslice", "tenant-service-name", discoveryv1.AddressTypeIPv4,
				*createPort("http", 80, v1.ProtocolTCP),
				[]discoveryv1.Endpoint{
					*createEndpoint("123.45.67.89", "worker-0-test", true, true, false),
				},
			)

			noSelectorSvcName := "svc-without-selector"
			svc := &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      noSelectorSvcName,
					Namespace: infraNamespace,
					Labels: map[string]string{
						kubevirt.TenantServiceNameLabelKey:      "tenant-service-name",
						kubevirt.TenantServiceNamespaceLabelKey: tenantNamespace,
						kubevirt.TenantClusterNameLabelKey:      "test-cluster",
					},
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{
							Name:       "web",
							Port:       80,
							NodePort:   31900,
							Protocol:   v1.ProtocolTCP,
							TargetPort: intstr.IntOrString{IntVal: 30390},
						},
					},
					Type:                  v1.ServiceTypeLoadBalancer,
					ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyLocal,
				},
			}

			_, err := testVals.infraClient.CoreV1().Services(infraNamespace).Create(context.TODO(), svc, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err := testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).
					List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false, err
				}
				return len(epsList.Items) == 1, nil
			}).Should(BeTrue(), "Controller should create an EndpointSlice in infra cluster for the no-selector LB service")

			svcWithSelector, err := testVals.infraClient.CoreV1().Services(infraNamespace).Get(
				context.TODO(), noSelectorSvcName, metav1.GetOptions{})
			Expect(err).To(BeNil())

			svcWithSelector.Spec.Selector = map[string]string{"app": "test-value"}
			_, err = testVals.infraClient.CoreV1().Services(infraNamespace).
				Update(context.TODO(), svcWithSelector, metav1.UpdateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() (bool, error) {
				epsList, err := testVals.infraClient.DiscoveryV1().EndpointSlices(infraNamespace).
					List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					return false, err
				}
				return len(epsList.Items) == 0, nil
			}).Should(BeTrue(), "All EndpointSlices should be removed after Service acquires a selector (no new slices created)")
		})
	})
})
