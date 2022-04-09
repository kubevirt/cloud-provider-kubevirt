package kubevirt

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/cloudprovider/kubevirt/mock/client"
)

var (
	emptyZone                   = cloudprovider.Zone{}
	invalidNodeVMI              = kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{Name: "invalidNode", Namespace: "test"}, Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "invalidNode"}}
	noFailureDomainRegionVMI    = kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{Name: "noFailureDomainRegion", Namespace: "test"}, Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "noFailureDomainRegion"}}
	validFailureDomainRegionVMI = kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{Name: "validFailureDomainRegion", Namespace: "test"}, Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "validFailureDomainRegion"}}
	validFailureDomainVMI       = kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{Name: "validFailureDomain", Namespace: "test"}, Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "validFailureDomain"}}
	validRegionVMI              = kubevirtv1.VirtualMachineInstance{ObjectMeta: metav1.ObjectMeta{Name: "validRegion", Namespace: "test"}, Status: kubevirtv1.VirtualMachineInstanceStatus{NodeName: "validRegion"}}

	noFailureDomainRegionNode    = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "noFailureDomainRegion"}}
	validFailureDomainRegionNode = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "validFailureDomainRegion", Labels: map[string]string{"failure-domain.beta.kubernetes.io/zone": "failureDomain", "failure-domain.beta.kubernetes.io/region": "region"}}}
	validFailureDomainNode       = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "validFailureDomain", Labels: map[string]string{"failure-domain.beta.kubernetes.io/zone": "failureDomain"}}}
	validRegionNode              = corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "validRegion", Labels: map[string]string{"failure-domain.beta.kubernetes.io/region": "region"}}}
)

func TestGetZone(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)
	z := &zones{
		namespace: "test",
		client:    c,
	}

	// The kubevirt cloud provider is an external cloud provider
	// and does therefore not implement this method
	zone, err := z.GetZone(ctx)

	if zone != emptyZone {
		t.Errorf("Expected: %v, got: %v", emptyZone, zone)
	}
	if err != cloudprovider.NotImplemented {
		t.Errorf("Expected: %v, got %v", cloudprovider.NotImplemented, err)
	}
}

func TestGetZoneByProviderID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)
	z := &zones{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "invalidNode", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, invalidNodeVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "invalidNode"}, gomock.AssignableToTypeOf(&corev1.Node{})).Return(errors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "invalidNode")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "noFailureDomainRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, noFailureDomainRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "noFailureDomainRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, noFailureDomainRegionNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomainRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validFailureDomainRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomainRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validFailureDomainRegionNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomain", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validFailureDomainVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomain"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validFailureDomainNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validRegionNode),
	)

	tests := []struct {
		providerID    string
		expectedZone  cloudprovider.Zone
		expectedError error
	}{
		{"notkubevirt://instance", emptyZone, fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
		{"kubevirt://missingVMI", emptyZone, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{"kubevirt://invalidNode", emptyZone, fmt.Errorf("nodes \"invalidNode\" not found")},
		{"kubevirt://noFailureDomainRegion", emptyZone, nil},
		{"kubevirt://validFailureDomainRegion", cloudprovider.Zone{FailureDomain: "failureDomain", Region: "region"}, nil},
		{"kubevirt://validFailureDomain", cloudprovider.Zone{FailureDomain: "failureDomain", Region: ""}, nil},
		{"kubevirt://validRegion", cloudprovider.Zone{FailureDomain: "", Region: "region"}, nil},
	}

	for _, test := range tests {
		zone, err := z.GetZoneByProviderID(ctx, test.providerID)
		if zone != test.expectedZone {
			t.Errorf("Expected: %v, got: %v", test.expectedZone, zone)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestGetZoneByNodeName(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)
	z := &zones{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "invalidNode", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, invalidNodeVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "invalidNode"}, gomock.AssignableToTypeOf(&corev1.Node{})).Return(errors.NewNotFound(schema.GroupResource{Group: "", Resource: "nodes"}, "invalidNode")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "noFailureDomainRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, noFailureDomainRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "noFailureDomainRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, noFailureDomainRegionNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomainRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validFailureDomainRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomainRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validFailureDomainRegionNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomain", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validFailureDomainVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validFailureDomain"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validFailureDomainNode),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validRegion", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, validRegionVMI),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "validRegion"}, gomock.AssignableToTypeOf(&corev1.Node{})).SetArg(2, validRegionNode),
	)

	tests := []struct {
		nodeName      types.NodeName
		expectedZone  cloudprovider.Zone
		expectedError error
	}{
		{types.NodeName("missingVMI"), emptyZone, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{types.NodeName("invalidNode"), emptyZone, fmt.Errorf("nodes \"invalidNode\" not found")},
		{types.NodeName("noFailureDomainRegion"), emptyZone, nil},
		{types.NodeName("validFailureDomainRegion"), cloudprovider.Zone{FailureDomain: "failureDomain", Region: "region"}, nil},
		{types.NodeName("validFailureDomain"), cloudprovider.Zone{FailureDomain: "failureDomain", Region: ""}, nil},
		{types.NodeName("validRegion"), cloudprovider.Zone{FailureDomain: "", Region: "region"}, nil},
	}

	for _, test := range tests {
		zone, err := z.GetZoneByNodeName(ctx, test.nodeName)
		if zone != test.expectedZone {
			t.Errorf("Expected: %v, got: %v", test.expectedZone, zone)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}
