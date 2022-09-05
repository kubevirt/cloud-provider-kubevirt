package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/provider/mock/client"
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
		namespace: "test",
		client:    c,
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
		namespace: "test",
		client:    c,
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
		namespace: "test",
		client:    c,
		config: LoadBalancerConfig{
			CreationPollInterval: 1,
		},
	}

	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, lbServiceName)
	tenantService := &corev1.Service{
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
	nodes := []*corev1.Node{}

	tests := []struct {
		testName         string
		tenantService    *corev1.Service
		checkSvcExistErr error
		getCount         int
		portsChanged     bool
		updateSvcErr     error
		createSvcErr     error
		pollSvcErr       error
		expectedError    error
	}{
		{
			testName:         "Create New Service success - poll LoadBalancer service times 1",
			checkSvcExistErr: notFoundErr,
			getCount:         1,
		},
		{
			testName:         "Create New Service success - poll LoadBalancer service times 3",
			checkSvcExistErr: notFoundErr,
			getCount:         3,
		},
		{
			testName:         "Check Service Exist Error",
			checkSvcExistErr: errors.New("Test error - check if service already exist"),
			expectedError:    errors.New("Test error - check if service already exist"),
		},
		{
			testName:     "Update Exist Service Success",
			portsChanged: true,
		},
		{
			testName:     "Update Exist Service Success, ports not changed",
			portsChanged: false,
		},
		{
			testName:      "Update Exist Service Error",
			updateSvcErr:  errors.New("Test error - update Service"),
			expectedError: errors.New("Test error - update Service"),
			portsChanged:  true,
		},
		{
			testName:         "Create New Service Error",
			checkSvcExistErr: notFoundErr,
			createSvcErr:     errors.New("Test error - Create Service"),
			expectedError:    errors.New("Test error - Create Service"),
		},
		{
			testName:         "Create New Service Error - poll LoadBalancer service Error",
			checkSvcExistErr: notFoundErr,
			getCount:         1,
			pollSvcErr:       errors.New("Test error - poll Service"),
			expectedError:    errors.New("Test error - poll Service"),
		},
		// In order to test it, need to increase the test timeout
		// {
		// 	testName:         "Create New Service Error - poll LoadBalancer service times 20",
		// 	checkSvcExistErr: notFoundErr,
		// 	getCount:         20,
		// },
	}

	loadBalancerIP := "123.456.7.8"

	for _, test := range tests {
		port := 30001
		if test.portsChanged {
			port = 30002
		}
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
		).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object) {
			if test.checkSvcExistErr == nil {
				copyService(infraServiceExist, obj.(*corev1.Service))
			}
		}).Return(test.checkSvcExistErr)
		if test.checkSvcExistErr == nil || test.checkSvcExistErr == notFoundErr {
			infraService1 := generateInfraService(
				tenantService,
				[]corev1.ServicePort{
					{Name: "port1", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 30001}},
				},
			)
			if test.checkSvcExistErr == nil {
				infraService1.Status = corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: loadBalancerIP,
							},
						},
					},
				}
				if test.portsChanged {
					c.EXPECT().Update(ctx, infraService1).Return(test.updateSvcErr)
				}
			} else {
				c.EXPECT().Create(ctx, infraService1).Return(test.createSvcErr)

				if test.createSvcErr == nil {
					for i := 0; i < test.getCount; i++ {
						infraService2 := &corev1.Service{}
						copyService(infraService1, infraService2)
						if i == test.getCount-1 {
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
						).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object) {
							copyService(infraService2, obj.(*corev1.Service))
						}).Return(test.pollSvcErr)
					}
				}
			}
		}

		lbStatus, err := lb.EnsureLoadBalancer(ctx, clusterName, tenantService, nodes)
		if test.expectedError == nil {
			if len(lbStatus.Ingress) != 1 {
				t.Errorf("Expected: 1 loadbalancer ingress, got %d loadbalancer ingress", len(lbStatus.Ingress))
			}
			if lbStatus.Ingress[0].IP != loadBalancerIP {
				t.Errorf("Expected: '%v', got: '%v'", loadBalancerIP, lbStatus.Ingress[0].IP)
			}
		} else {
			if err == nil || err.Error() != test.expectedError.Error() {
				t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
			}
		}
	}
}

func TestUpdateLoadBalancer(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace: "test",
		client:    c,
		config: LoadBalancerConfig{
			CreationPollInterval: 1,
		},
	}

	tenantService := &corev1.Service{
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
	nodes := []*corev1.Node{}
	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, lbServiceName)

	tests := []struct {
		testName      string
		tenantService *corev1.Service
		getSvcErr     error
		updateSvcErr  error
		expectedError error
		portsChanged  bool
	}{
		{
			testName:     "Update Service Success",
			portsChanged: true,
		},
		{
			testName:     "Update Service Success, ports not changed",
			portsChanged: false,
		},
		{
			testName:      "Get Service Error",
			getSvcErr:     errors.New("Test error - Get Service"),
			expectedError: errors.New("Test error - Get Service"),
			portsChanged:  true,
		},
		{
			testName:      "Get Service NotFound Error",
			getSvcErr:     notFoundErr,
			expectedError: notFoundErr,
			portsChanged:  true,
		},
		{
			testName:      "Update Service Error",
			updateSvcErr:  errors.New("Test error - update Service"),
			expectedError: errors.New("Test error - update Service"),
			portsChanged:  true,
		},
	}

	loadBalancerIP := "123.456.7.8"

	for _, test := range tests {
		port := 30001
		if test.portsChanged {
			port = 30002
		}
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
		).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object) {
			if test.getSvcErr == nil {
				copyService(infraServiceExist, obj.(*corev1.Service))
			}
		}).Return(test.getSvcErr)
		if test.getSvcErr == nil {
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
			if test.portsChanged {
				c.EXPECT().Update(ctx, infraService1).Return(test.updateSvcErr)
			}
		}

		err := lb.UpdateLoadBalancer(ctx, clusterName, tenantService, nodes)
		if test.expectedError == nil {
			if err != nil {
				t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
			}
		} else {
			if err == nil || err.Error() != test.expectedError.Error() {
				t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
			}
		}
	}
}

func generateInfraService(tenantSvc *corev1.Service, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbServiceName,
			Namespace: lbServiceNamespace,
			Labels: map[string]string{
				"cluster.x-k8s.io/tenant-service-name":      tenantSvc.Name,
				"cluster.x-k8s.io/tenant-service-namespace": tenantSvc.Namespace,
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

func copyService(src *corev1.Service, dst *corev1.Service) {
	dst.ObjectMeta = src.ObjectMeta
	dst.Spec = src.Spec
	dst.Status = src.Status
}

func TestEnsureLoadBalancerDeleted(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	lb := &loadbalancer{
		namespace: "test",
		client:    c,
		config: LoadBalancerConfig{
			CreationPollInterval: 1,
		},
	}

	tenantService := &corev1.Service{
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
	notFoundErr := apierrors.NewNotFound(schema.GroupResource{Group: "v1", Resource: "services"}, lbServiceName)

	tests := []struct {
		testName      string
		tenantService *corev1.Service
		getSvcErr     error
		deleteSvcErr  error
		expectedError error
	}{
		{
			testName: "Delete Service Success",
		},
		{
			testName:  "Delete Service Success - service doesn't exist",
			getSvcErr: notFoundErr,
		},
		{
			testName:      "Get Service Error",
			getSvcErr:     errors.New("Test error - Get Service"),
			expectedError: errors.New("Test error - Get Service"),
		},
		{
			testName:      "Delete Service Error",
			deleteSvcErr:  errors.New("Test error - Delete Service"),
			expectedError: errors.New("Test error - Delete Service"),
		},
	}

	loadBalancerIP := "123.456.7.8"

	for _, test := range tests {
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
		).Do(func(ctx context.Context, key client.ObjectKey, obj client.Object) {
			if test.getSvcErr == nil {
				copyService(infraServiceExist, obj.(*corev1.Service))
			}
		}).Return(test.getSvcErr)
		if test.getSvcErr == nil {
			c.EXPECT().Delete(ctx, infraServiceExist).Return(test.deleteSvcErr)
		}

		err := lb.EnsureLoadBalancerDeleted(ctx, clusterName, tenantService)
		if test.expectedError == nil {
			if err != nil {
				t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
			}
		} else {
			if err == nil || err.Error() != test.expectedError.Error() {
				t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
			}
		}
	}
}
