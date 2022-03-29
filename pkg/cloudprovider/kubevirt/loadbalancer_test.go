package kubevirt

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubevirtv1 "kubevirt.io/client-go/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/cloudprovider/kubevirt/mock/client"
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
	svc2 = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "a354699682ec311e9b210d663bd873d9",
			Namespace:   "test",
			Annotations: map[string]string{"foo": "bar"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30002}},
			},
			Selector: map[string]string{
				"cloud.kubevirt.io/a354699682ec311e9b210d663bd873d9": "service2",
			},
			Type:                  corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
		},
	}
	svc3 = corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "a35469a9e2ec311e9b210d663bd873d9",
			Namespace: "test",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30003}},
				{Name: "port2", Protocol: corev1.ProtocolTCP, Port: 443, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30004}},
			},
			Selector: map[string]string{
				"cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9": "service3",
			},
			Type: corev1.ServiceTypeLoadBalancer,
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
	vmiNode1 = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node1",
			Namespace: "test",
			UID:       types.UID("3546a480-2ec3-11e9-b210-d663bd873d93"),
			Labels: map[string]string{
				"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
			},
		},
	}
	vmiNode2 = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node2",
			Namespace: "test",
			UID:       types.UID("3546a354-2ec3-11e9-b210-d663bd873d93"),
			Labels: map[string]string{
				"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4",
				"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
			},
		},
	}
	vmiNode3 = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node3",
			Namespace: "test",
			UID:       types.UID("3546a228-2ec3-11e9-b210-d663bd873d93"),
			Labels:    make(map[string]string),
		},
	}
	vmiNode4 = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node4",
			Namespace: "test",
			UID:       types.UID("35469f8a-2ec3-11e9-b210-d663bd873d93"),
			Labels: map[string]string{
				"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
			},
		},
	}
	vmiNodeList = kubevirtv1.VirtualMachineInstanceList{
		Items: []kubevirtv1.VirtualMachineInstance{
			vmiNode1, vmiNode2, vmiNode3, vmiNode4,
		},
	}
	podVirtLauncherNode1 = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-node1-test",
			Namespace: "test",
			Labels: map[string]string{
				"kubevirt.io/created-by":                             "3546a480-2ec3-11e9-b210-d663bd873d93",
				"cloud.kubevirt.io/a6fa1a2662ad011e9b210d663bd873d9": "service4",
			},
		},
	}
	podVirtLauncherNode2 = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-node2-test",
			Namespace: "test",
			Labels: map[string]string{
				"kubevirt.io/created-by":                             "3546a354-2ec3-11e9-b210-d663bd873d93",
				"cloud.kubevirt.io/a6fa1a2662ad011e9b210d663bd873d9": "service4",
				"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
			},
		},
	}
	podVirtLauncherNode3 = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-node3-test",
			Namespace: "test",
			Labels: map[string]string{
				"kubevirt.io/created-by": "3546a228-2ec3-11e9-b210-d663bd873d93",
			},
		},
	}
	podVirtLauncherNode4 = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virt-launcher-node4-test",
			Namespace: "test",
			Labels: map[string]string{
				"kubevirt.io/created-by":                             "35469f8a-2ec3-11e9-b210-d663bd873d93",
				"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1",
			},
		},
	}
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

func TestGetLoadBalancer(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace:           "test",
		cloudProviderClient: c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a2662ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svcEmptyStatus),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a6582ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svcHostnameIngress),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "adoesnotexistink8s", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).Return(apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, "adoesnotexistink8s")),
	)

	tests := []struct {
		serviceUID       types.UID
		clusterName      string
		expectedLBStatus *corev1.LoadBalancerStatus
		expectedExists   bool
		expectedError    error
	}{
		{types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", makeLoadBalancerStatus([]string{}, []string{}), true, nil},
		{types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", makeLoadBalancerStatus([]string{"192.168.0.35"}, []string{}), true, nil},
		{types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", makeLoadBalancerStatus([]string{"192.168.0.36"}, []string{"lb1.example.com", "lb2.example.com"}), true, nil},
		{types.UID("does-not-exist-in-k8s"), "cluster2", nil, false, nil},
	}

	for _, test := range tests {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: test.serviceUID}}
		status, exists, err := lb.GetLoadBalancer(ctx, test.clusterName, svc)
		if !cmpLoadBalancerStatuses(status, test.expectedLBStatus) {
			t.Errorf("Expected: %v, got: %v", test.expectedLBStatus, status)
		}
		if test.expectedExists != exists {
			t.Errorf("Expected: '%v', got '%v'", test.expectedExists, exists)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestGetLoadBalancerName(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace:           "test",
		cloudProviderClient: c,
	}

	tests := []struct {
		serviceUID   types.UID
		clusterName  string
		expectedName string
	}{
		{types.UID("6fa1a266-2ad0-11e9-b210-d663bd873d93"), "", "a6fa1a2662ad011e9b210d663bd873d9"},
		{types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"), "cluster1", "a6fa1a50e2ad011e9b210d663bd873d9"},
		{types.UID("6fa1a658-2ad0-11e9-b210-d663bd873d93"), "cluster2", "a6fa1a6582ad011e9b210d663bd873d9"},
	}

	for _, test := range tests {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{UID: test.serviceUID}}
		name := lb.GetLoadBalancerName(ctx, test.clusterName, svc)
		if name != test.expectedName {
			t.Errorf("Expected: %v, got: %v", test.expectedName, name)
		}
	}
}

func TestEnsureLoadBalancer(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace:           "test",
		cloudProviderClient: c,
		config: LoadBalancerConfig{
			CreationPollInterval: 1,
		},
	}

	vmiNode1LabeledForSvc2 := vmiNode1
	vmiNode1LabeledForSvc2.ObjectMeta.Labels["cloud.kubevirt.io/a354699682ec311e9b210d663bd873d9"] = "service2"
	createdByVMINode1Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINode1Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINode1Req)}
	podVirtLauncherNode1LabeledForSvc2 := podVirtLauncherNode1
	podVirtLauncherNode1LabeledForSvc2.ObjectMeta.Labels["cloud.kubevirt.io/a354699682ec311e9b210d663bd873d9"] = "service2"
	listOptionsSvc2 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/a354699682ec311e9b210d663bd873d9": "service2"})}
	svc2WithStatus := svc2
	svc2WithStatus.Status = corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "192.168.0.38"}}}}

	vmiNode1LabeledForSvc3 := vmiNode1
	vmiNode1LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	vmiNode2LabeledForSvc3 := vmiNode2
	vmiNode2LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	vmiNode3LabeledForSvc3 := vmiNode3
	vmiNode3LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	createdByVMINodes123Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93", "3546a354-2ec3-11e9-b210-d663bd873d93", "3546a228-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINodes123Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINodes123Req)}
	podVirtLauncherNode1LabeledForSvc3 := podVirtLauncherNode1
	podVirtLauncherNode1LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	podVirtLauncherNode2LabeledForSvc3 := podVirtLauncherNode2
	podVirtLauncherNode2LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	podVirtLauncherNode3LabeledForSvc3 := podVirtLauncherNode3
	podVirtLauncherNode3LabeledForSvc3.ObjectMeta.Labels["cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9"] = "service3"
	listOptionsSvc3 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/a35469a9e2ec311e9b210d663bd873d9": "service3"})}
	svc3WithStatus := svc3
	svc3WithStatus.Status = corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "192.168.0.39"}}}}

	vmiNode3LabeledForSvc4 := vmiNode3
	vmiNode3LabeledForSvc4.ObjectMeta.Labels["cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9"] = "service4"
	podVirtLauncherNode3LabeledForSvc4 := podVirtLauncherNode3
	podVirtLauncherNode3LabeledForSvc4.ObjectMeta.Labels["cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9"] = "service4"
	createdByVMINodes12Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93", "3546a354-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINodes12Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINodes12Req)}
	listOptionsSvc4 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4"})}
	svc4WithNewPort := svc4
	svc4WithNewPort.Spec.Ports = append(svc4WithNewPort.Spec.Ports, corev1.ServicePort{Name: "port2", Protocol: corev1.ProtocolTCP, Port: 443, TargetPort: intstr.FromInt(30006)})

	vmiNode2NotLabeledForSvc4 := vmiNode2
	delete(vmiNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	podVirtLauncherNode2NotLabeledForSvc4 := podVirtLauncherNode2
	delete(podVirtLauncherNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	svc4WithoutPorts := svc4
	svc4WithoutPorts.Spec.Ports = []corev1.ServicePort{}

	gomock.InOrder(
		// Testcase: No LB exists for service, no nodes given
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),

		// Testcase: No LB exists for service, single node given
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1LabeledForSvc2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINode1Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1LabeledForSvc2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc2).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1LabeledForSvc2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc2).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1LabeledForSvc2}}),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a354699682ec311e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).Return(apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, "a354699682ec311e9b210d663bd873d9")),
		c.EXPECT().Create(ctx, &svc2),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a354699682ec311e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc2WithStatus),

		// Testcase: No LB exists for service, three nodes given
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1LabeledForSvc3),
		c.EXPECT().Update(ctx, &vmiNode2LabeledForSvc3),
		c.EXPECT().Update(ctx, &vmiNode3LabeledForSvc3),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINodes123Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2, podVirtLauncherNode3}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1LabeledForSvc3),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2LabeledForSvc3),
		c.EXPECT().Update(ctx, &podVirtLauncherNode3LabeledForSvc3),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc3).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1LabeledForSvc3, vmiNode2LabeledForSvc3, vmiNode3LabeledForSvc3}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc3).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1LabeledForSvc3, podVirtLauncherNode2LabeledForSvc3, podVirtLauncherNode3LabeledForSvc3}}),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a35469a9e2ec311e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).Return(apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, "a35469a9e2ec311e9b210d663bd873d9")),
		c.EXPECT().Create(ctx, &svc3),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a35469a9e2ec311e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc3WithStatus),

		// Testcase: LB already exists for service, unchanged amount of nodes
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().Update(ctx, &vmiNode2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINodes12Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
		c.EXPECT().Update(ctx, &svc4),

		// Testcase: LB already exists for service, new nodes given
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().Update(ctx, &vmiNode2),
		c.EXPECT().Update(ctx, &vmiNode3LabeledForSvc4),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINodes123Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2, podVirtLauncherNode3}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2),
		c.EXPECT().Update(ctx, &podVirtLauncherNode3LabeledForSvc4),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2, vmiNode3LabeledForSvc4}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2, podVirtLauncherNode3LabeledForSvc4}}),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
		c.EXPECT().Update(ctx, &svc4WithNewPort),

		// Testcase: LB already exists for service, nodes removed
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINode1Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Update(ctx, &vmiNode2NotLabeledForSvc4),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2NotLabeledForSvc4),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
		c.EXPECT().Update(ctx, &svc4WithoutPorts),
	)

	tests := []struct {
		service                       *corev1.Service
		nodes                         []*corev1.Node
		clusterName                   string
		expectedLoadBalancerIP        string
		expectedAnnotations           map[string]string
		expectedExternalTrafficPolicy corev1.ServiceExternalTrafficPolicyType
		expectedError                 error
	}{
		// Testcase: No LB exists for service, no nodes given
		// Expected: LB Service will be created, no VMIs & pods labelled, error "Failed to create Pod label selector"
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001},
					},
				},
			},
			[]*corev1.Node{},
			"",
			"",
			nil,
			corev1.ServiceExternalTrafficPolicyTypeCluster,
			errors.New("Failed to create Pod label selector: values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty"),
		},
		// Testcase: No LB exists for service, single node given, annotation on original service
		// Expected: LB Service will be created, one VMI & pod labelled, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "service2",
					UID:         types.UID("35469968-2ec3-11e9-b210-d663bd873d93"),
					Annotations: map[string]string{"foo": "bar"},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30002},
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
			},
			"cluster1",
			"192.168.0.38",
			map[string]string{"foo": "bar"},
			corev1.ServiceExternalTrafficPolicyTypeLocal,
			nil,
		},
		// Testcase: No LB exists for service, three nodes given
		// Expected: LB Service will be created, three VMIs & pods labelled, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service3",
					UID:  types.UID("35469a9e-2ec3-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30003},
						{Name: "port2", Protocol: corev1.ProtocolTCP, Port: 443, NodePort: 30004},
					},
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node3"}},
			},
			"cluster1",
			"192.168.0.39",
			nil,
			corev1.ServiceExternalTrafficPolicyTypeCluster,
			nil,
		},
		// Testcase: LB already exists for service, unchanged amount of nodes
		// Expected: LB Service unchanged, labels on VMIs & pods unchanged, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30005},
					},
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node2"}},
			},
			"cluster2",
			"192.168.0.35",
			nil,
			corev1.ServiceExternalTrafficPolicyTypeCluster,
			nil,
		},
		// Testcase: LB already exists for service, new nodes given
		// Expected: LB Service unchanged, new VMIs & pods labelled, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30005},
						{Name: "port2", Protocol: corev1.ProtocolTCP, Port: 443, NodePort: 30006},
					},
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node3"}},
			},
			"cluster2",
			"192.168.0.35",
			nil,
			corev1.ServiceExternalTrafficPolicyTypeCluster,
			nil,
		},
		// Testcase: LB already exists for service, nodes removed
		// Expected: LB Service unchanged, labels removed from VMIs & pods, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
			},
			"cluster2",
			"192.168.0.35",
			nil,
			corev1.ServiceExternalTrafficPolicyTypeCluster,
			nil,
		},
	}

	for _, test := range tests {
		lbStatus, err := lb.EnsureLoadBalancer(ctx, test.clusterName, test.service, test.nodes)
		if test.expectedLoadBalancerIP != "" {
			if lbStatus.Ingress == nil {
				t.Error("Expected: 1 loadbalancer ingress, got undefined loadbalancer ingress")
			}
			if len(lbStatus.Ingress) != 1 {
				t.Errorf("Expected: 1 loadbalancer ingress, got %d loadbalancer ingress", len(lbStatus.Ingress))
			}
			if test.expectedLoadBalancerIP != lbStatus.Ingress[0].IP {
				t.Errorf("Expected: '%v', got: '%v'", test.expectedLoadBalancerIP, lbStatus.Ingress[0].IP)
			}
			if !reflect.DeepEqual(test.expectedAnnotations, test.service.ObjectMeta.Annotations) {
				t.Errorf("Expected: annotations '%s', got '%s'", test.expectedAnnotations, test.service.ObjectMeta.Annotations)
			}
			if test.service.Spec.ExternalTrafficPolicy != "" && test.expectedExternalTrafficPolicy != test.service.Spec.ExternalTrafficPolicy {
				t.Errorf("Expected: ExternalTrafficPolicy '%s', got '%s'", test.expectedExternalTrafficPolicy, test.service.Spec.ExternalTrafficPolicy)
			}
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestUpdateLoadBalancer(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace:           "test",
		cloudProviderClient: c,
	}

	createdByVMINodes12Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93", "3546a354-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINodes12Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINodes12Req)}
	listOptionsSvc4 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4"})}

	vmiNode3LabeledForSvc4 := vmiNode3
	vmiNode3LabeledForSvc4.ObjectMeta.Labels["cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9"] = "service4"
	podVirtLauncherNode3LabeledForSvc4 := podVirtLauncherNode3
	podVirtLauncherNode3LabeledForSvc4.ObjectMeta.Labels["cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9"] = "service4"
	createdByVMINodes123Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93", "3546a354-2ec3-11e9-b210-d663bd873d93", "3546a228-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINodes123Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINodes123Req)}

	vmiNode2NotLabeledForSvc4 := vmiNode2
	delete(vmiNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	podVirtLauncherNode2NotLabeledForSvc4 := podVirtLauncherNode2
	delete(podVirtLauncherNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	createdByVMINode1Req, _ := labels.NewRequirement("kubevirt.io/created-by", selection.In, []string{"3546a480-2ec3-11e9-b210-d663bd873d93"})
	createdByVMINode1Selector := client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*createdByVMINode1Req)}

	gomock.InOrder(
		// Testcase: unchanged amount of nodes
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().Update(ctx, &vmiNode2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINodes12Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),

		// Testcase: new nodes given
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().Update(ctx, &vmiNode2),
		c.EXPECT().Update(ctx, &vmiNode3LabeledForSvc4),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINodes123Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2, podVirtLauncherNode3}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2),
		c.EXPECT().Update(ctx, &podVirtLauncherNode3LabeledForSvc4),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2, vmiNode3LabeledForSvc4}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2, podVirtLauncherNode3LabeledForSvc4}}),

		// Testcase: nodes removed
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiNodeList),
		c.EXPECT().Update(ctx, &vmiNode1),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), client.InNamespace("test"), createdByVMINode1Selector).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1}}),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Update(ctx, &vmiNode2NotLabeledForSvc4),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2NotLabeledForSvc4),
	)

	tests := []struct {
		service       *corev1.Service
		nodes         []*corev1.Node
		clusterName   string
		expectedError error
	}{
		// Testcase: unchanged amount of nodes
		// Expected: labels on VMIs & pods unchanged, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30005},
					},
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node2"}},
			},
			"cluster2",
			nil,
		},
		// Testcase: new nodes given
		// Expected: new VMIs & pods labelled, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30005},
						{Name: "port2", Protocol: corev1.ProtocolTCP, Port: 443, NodePort: 30006},
					},
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node3"}},
			},
			"cluster2",
			nil,
		},
		// Testcase: nodes removed
		// Expected: labels removed from VMIs & pods, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			[]*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}, Spec: corev1.NodeSpec{ProviderID: "kubevirt://node1"}},
			},
			"cluster2",
			nil,
		},
	}

	for _, test := range tests {
		err := lb.UpdateLoadBalancer(ctx, test.clusterName, test.service, test.nodes)
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestEnsureLoadBalancerDeleted(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace:           "test",
		cloudProviderClient: c,
	}

	listOptionsSvc1 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9": "service1"})}
	vmiNode2NotLabeledForSvc1 := vmiNode2
	delete(vmiNode2NotLabeledForSvc1.ObjectMeta.Labels, "cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9")
	vmiNode4NotLabeledForSvc1 := vmiNode4
	delete(vmiNode4NotLabeledForSvc1.ObjectMeta.Labels, "cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9")
	podVirtLauncherNode2NotLabeledForSvc1 := podVirtLauncherNode2
	delete(podVirtLauncherNode2NotLabeledForSvc1.ObjectMeta.Labels, "cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9")
	podVirtLauncherNode4NotLabeledForSvc1 := podVirtLauncherNode4
	delete(podVirtLauncherNode4NotLabeledForSvc1.ObjectMeta.Labels, "cloud.kubevirt.io/af6ebf1722bb111e9b210d663bd873d9")

	listOptionsSvc4 := &client.ListOptions{Namespace: "test", LabelSelector: labels.SelectorFromSet(labels.Set{"cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9": "service4"})}
	vmiNode1NotLabeledForSvc4 := vmiNode1
	delete(vmiNode1NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	vmiNode2NotLabeledForSvc4 := vmiNode2
	delete(vmiNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	podVirtLauncherNode1NotLabeledForSvc4 := podVirtLauncherNode1
	delete(podVirtLauncherNode1NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")
	podVirtLauncherNode2NotLabeledForSvc4 := podVirtLauncherNode2
	delete(podVirtLauncherNode2NotLabeledForSvc4.ObjectMeta.Labels, "cloud.kubevirt.io/a6fa1a50e2ad011e9b210d663bd873d9")

	gomock.InOrder(
		// Testcase: No LB exists for service
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "af6ebf1722bb111e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).Return(apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, "af6ebf1722bb111e9b210d663bd873d9")),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc1).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode2, vmiNode4}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc1).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode2, podVirtLauncherNode4}}),
		c.EXPECT().Update(ctx, &vmiNode2NotLabeledForSvc1),
		c.EXPECT().Update(ctx, &vmiNode4NotLabeledForSvc1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2NotLabeledForSvc1),
		c.EXPECT().Update(ctx, &podVirtLauncherNode4NotLabeledForSvc1),

		// Testcase: LB exists for service
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "a6fa1a50e2ad011e9b210d663bd873d9", Namespace: "test"}, gomock.AssignableToTypeOf(&corev1.Service{})).SetArg(2, svc4),
		c.EXPECT().Delete(ctx, &svc4),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), listOptionsSvc4).SetArg(1, kubevirtv1.VirtualMachineInstanceList{Items: []kubevirtv1.VirtualMachineInstance{vmiNode1, vmiNode2}}),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&corev1.PodList{}), listOptionsSvc4).SetArg(1, corev1.PodList{Items: []corev1.Pod{podVirtLauncherNode1, podVirtLauncherNode2}}),
		c.EXPECT().Update(ctx, &vmiNode1NotLabeledForSvc4),
		c.EXPECT().Update(ctx, &vmiNode2NotLabeledForSvc4),
		c.EXPECT().Update(ctx, &podVirtLauncherNode1NotLabeledForSvc4),
		c.EXPECT().Update(ctx, &podVirtLauncherNode2NotLabeledForSvc4),
	)

	tests := []struct {
		service       *corev1.Service
		clusterName   string
		expectedError error
	}{
		// Testcase: No LB exists for service
		// Expected: labels removed from VMIs & pods, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service1",
					UID:  types.UID("f6ebf172-2bb1-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001},
					},
				},
			},
			"cluster1",
			nil,
		},
		// Testcase: LB exists for service
		// Expected: LB deleted, labels removed from VMIs & pods, no error
		{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "service4",
					UID:  types.UID("6fa1a50e-2ad0-11e9-b210-d663bd873d93"),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30005},
					},
				},
			},
			"cluster2",
			nil,
		},
	}

	for _, test := range tests {
		err := lb.EnsureLoadBalancerDeleted(ctx, test.clusterName, test.service)
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}
