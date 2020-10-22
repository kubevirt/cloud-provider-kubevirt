package kubevirt

// import (
// 	"context"
// 	"fmt"
// 	"testing"

// 	"github.com/golang/mock/gomock"
// 	corev1 "k8s.io/api/core/v1"
// 	"k8s.io/apimachinery/pkg/api/errors"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	"k8s.io/apimachinery/pkg/runtime/schema"
// 	"k8s.io/apimachinery/pkg/types"
// 	"k8s.io/client-go/kubernetes/fake"
// 	cloudprovider "k8s.io/cloud-provider"
// 	kubevirtv1 "kubevirt.io/client-go/api/v1"
// 	"kubevirt.io/client-go/kubecli"
// )

// func mockZones(t *testing.T, namespace string) cloudprovider.Zones {
// 	ctrl := gomock.NewController(t)

// 	nodeList := corev1.NodeList{
// 		Items: []corev1.Node{
// 			corev1.Node{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "noFailureDomainRegion",
// 				},
// 			},
// 			corev1.Node{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "validFailureDomainRegion",
// 					Labels: map[string]string{
// 						"failure-domain.beta.kubernetes.io/zone":   "failureDomain",
// 						"failure-domain.beta.kubernetes.io/region": "region",
// 					},
// 				},
// 			},
// 			corev1.Node{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "validFailureDomain",
// 					Labels: map[string]string{
// 						"failure-domain.beta.kubernetes.io/zone": "failureDomain",
// 					},
// 				},
// 			},
// 			corev1.Node{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name: "validRegion",
// 					Labels: map[string]string{
// 						"failure-domain.beta.kubernetes.io/region": "region",
// 					},
// 				},
// 			},
// 		},
// 	}

// 	kubevirt := kubecli.NewMockKubevirtClient(ctrl)

// 	kubernetes := fake.NewSimpleClientset(&nodeList)
// 	kubevirt.EXPECT().CoreV1().Return(kubernetes.CoreV1()).AnyTimes()

// 	vmiInterface := kubecli.NewMockVirtualMachineInstanceInterface(ctrl)
// 	kubevirt.EXPECT().VirtualMachineInstance(gomock.Eq(namespace)).Return(vmiInterface).AnyTimes()

// 	vmiMap := map[string]*kubevirtv1.VirtualMachineInstance{
// 		"invalidNode":              kubevirtv1.NewMinimalVMIWithNS(namespace, "invalidnode"),
// 		"noFailureDomainRegion":    kubevirtv1.NewMinimalVMIWithNS(namespace, "noFailureDomainRegion"),
// 		"validFailureDomainRegion": kubevirtv1.NewMinimalVMIWithNS(namespace, "validFailureDomainRegion"),
// 		"validFailureDomain":       kubevirtv1.NewMinimalVMIWithNS(namespace, "validFailureDomain"),
// 		"validRegion":              kubevirtv1.NewMinimalVMIWithNS(namespace, "validRegion"),
// 	}

// 	for name, vmi := range vmiMap {
// 		vmi.Status.NodeName = name
// 		vmiInterface.EXPECT().Get(gomock.Eq(name), gomock.Any()).Return(vmi, nil)
// 	}
// 	vmiInterface.EXPECT().Get(gomock.Eq("missingVMI"), gomock.Any()).Return(nil, errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI"))

// 	return &zones{
// 		namespace: namespace,
// 		kubevirt:  kubevirt,
// 	}
// }

// var emptyZone = cloudprovider.Zone{}

// func TestGetZone(t *testing.T) {
// 	// The kubevirt cloud provider is an external cloud provider
// 	// and does therefore not implement this method
// 	z := mockZones(t, "testGetZone")
// 	zone, err := z.GetZone(context.TODO())

// 	if zone != emptyZone {
// 		t.Errorf("Expected: %v, got: %v", emptyZone, zone)
// 	}
// 	if err != cloudprovider.NotImplemented {
// 		t.Errorf("Expected: %v, got %v", cloudprovider.NotImplemented, err)
// 	}
// }

// func TestGetZoneByProviderID(t *testing.T) {
// 	z := mockZones(t, "testGetZoneByProviderID")
// 	tests := []struct {
// 		providerID    string
// 		expectedZone  cloudprovider.Zone
// 		expectedError error
// 	}{
// 		{"notkubevirt://instance", emptyZone, fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
// 		{"kubevirt://missingVMI", emptyZone, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
// 		{"kubevirt://invalidNode", emptyZone, fmt.Errorf("nodes \"invalidNode\" not found")},
// 		{"kubevirt://noFailureDomainRegion", emptyZone, nil},
// 		{"kubevirt://validFailureDomainRegion", cloudprovider.Zone{FailureDomain: "failureDomain", Region: "region"}, nil},
// 		{"kubevirt://validFailureDomain", cloudprovider.Zone{FailureDomain: "failureDomain", Region: ""}, nil},
// 		{"kubevirt://validRegion", cloudprovider.Zone{FailureDomain: "", Region: "region"}, nil},
// 	}

// 	for _, test := range tests {
// 		zone, err := z.GetZoneByProviderID(context.TODO(), test.providerID)
// 		if zone != test.expectedZone {
// 			t.Errorf("Expected: %v, got: %v", test.expectedZone, zone)
// 		}
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 	}
// }

// func TestGetZoneByNodeName(t *testing.T) {
// 	z := mockZones(t, "testGetZoneByProviderID")
// 	tests := []struct {
// 		nodeName      types.NodeName
// 		expectedZone  cloudprovider.Zone
// 		expectedError error
// 	}{
// 		{types.NodeName("missingVMI"), emptyZone, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
// 		{types.NodeName("invalidNode"), emptyZone, fmt.Errorf("nodes \"invalidNode\" not found")},
// 		{types.NodeName("noFailureDomainRegion"), emptyZone, nil},
// 		{types.NodeName("validFailureDomainRegion"), cloudprovider.Zone{FailureDomain: "failureDomain", Region: "region"}, nil},
// 		{types.NodeName("validFailureDomain"), cloudprovider.Zone{FailureDomain: "failureDomain", Region: ""}, nil},
// 		{types.NodeName("validRegion"), cloudprovider.Zone{FailureDomain: "", Region: "region"}, nil},
// 	}

// 	for _, test := range tests {
// 		zone, err := z.GetZoneByNodeName(context.TODO(), test.nodeName)
// 		if zone != test.expectedZone {
// 			t.Errorf("Expected: %v, got: %v", test.expectedZone, zone)
// 		}
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 	}
// }
