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
	kubevirtv1 "kubevirt.io/client-go/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/cloudprovider/kubevirt/mock/client"
)

var (
	vmiNodeHostname = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeHostname",
			Namespace: "test",
		},
		Spec: kubevirtv1.VirtualMachineInstanceSpec{
			Hostname: "hostname",
		},
	}
	vmiNodeNoHostname = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeNoHostname",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{IP: "10.0.0.10"},
			},
		},
	}
	vmiNodeDomainHostname = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeDomainHostname",
			Namespace: "test",
		},
		Spec: kubevirtv1.VirtualMachineInstanceSpec{
			Hostname: "node.domainname",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{IPs: []string{"10.0.0.11"}},
			},
		},
	}
	vmiNodeDomainNoHostname = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeDomainNoHostname",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{IP: "10.0.0.12", IPs: []string{"10.0.0.13"}},
				{IPs: []string{"10.0.0.14", "10.0.0.15"}},
				{IP: "10.0.0.16", IPs: []string{"10.0.0.16", "10.0.0.17"}},
			},
		},
	}
	vmiNodePhaseUnset = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseUnset",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.VmPhaseUnset,
		},
	}
	vmiNodePhasePending = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhasePending",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Pending,
		},
	}
	vmiNodePhaseScheduling = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseScheduling",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Scheduling,
		},
	}
	vmiNodePhaseScheduled = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseScheduled",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Scheduled,
		},
	}
	vmiNodePhaseRunning = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseRunning",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Running,
		},
	}
	vmiNodePhaseSucceeded = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseSucceeded",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Succeeded,
		},
	}
	vmiNodePhaseFailed = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseFailed",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Failed,
		},
	}
	vmiNodePhaseUnknown = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodePhaseUnknown",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Phase: kubevirtv1.Unknown,
		},
	}
	vmiNodeFlavor = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeFlavor",
			Namespace: "test",
			Annotations: map[string]string{
				"cloud.kubevirt.io/flavor": "flavor",
			},
		},
	}
	vmiNodeNoFlavor = kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodeNoFlavor",
			Namespace: "test",
		},
	}
	vmiList = kubevirtv1.VirtualMachineInstanceList{
		Items: []kubevirtv1.VirtualMachineInstance{
			vmiNodeHostname,
			vmiNodeNoHostname,
			vmiNodeDomainHostname,
			vmiNodeDomainNoHostname,
			vmiNodePhaseUnset,
			vmiNodePhasePending,
			vmiNodePhaseScheduling,
			vmiNodePhaseScheduled,
			vmiNodePhaseRunning,
			vmiNodePhaseSucceeded,
			vmiNodePhaseFailed,
			vmiNodePhaseUnknown,
			vmiNodeFlavor,
			vmiNodeNoFlavor,
		},
	}
)

func makeNodeAddressList(hostname string, internalIPs []string) []corev1.NodeAddress {
	addresses := make([]corev1.NodeAddress, 0)
	if hostname != "" {
		addresses = append(addresses, corev1.NodeAddress{
			Type:    corev1.NodeHostName,
			Address: hostname,
		})
	}
	for _, ip := range internalIPs {
		addresses = append(addresses, corev1.NodeAddress{
			Type:    corev1.NodeInternalIP,
			Address: ip,
		})
	}
	return addresses
}

func cmpNodeAddresses(a, b []corev1.NodeAddress) bool {
	if len(a) != len(b) {
		return false
	}

	for i, v := range a {
		if (v.Address != b[i].Address) || (v.Type != b[i].Type) {
			return false
		}
	}

	return true
}

func TestNodeAddresses(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeNoHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeNoHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeDomainHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeDomainHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeDomainNoHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeDomainNoHostname),
	)

	tests := []struct {
		nodeName              types.NodeName
		expectedNodeAddresses []corev1.NodeAddress
		expectedError         error
	}{
		{types.NodeName("missingVMI"), nil, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{types.NodeName("nodeHostname"), makeNodeAddressList("hostname", nil), nil},
		{types.NodeName("nodeNoHostname"), makeNodeAddressList("", []string{"10.0.0.10"}), nil},
		{types.NodeName("nodeDomainHostname"), makeNodeAddressList("node.domainname", []string{"10.0.0.11"}), nil},
		{types.NodeName("nodeDomainNoHostname"), makeNodeAddressList("", []string{"10.0.0.12", "10.0.0.13", "10.0.0.14", "10.0.0.15", "10.0.0.16", "10.0.0.17"}), nil},
	}

	for _, test := range tests {
		nodeAddresses, err := i.NodeAddresses(ctx, test.nodeName)
		if !cmpNodeAddresses(nodeAddresses, test.expectedNodeAddresses) {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeAddresses, nodeAddresses)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestNodeAddressesByProviderID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeNoHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeNoHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeDomainHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeDomainHostname),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeDomainNoHostname", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeDomainNoHostname),
	)

	tests := []struct {
		providerID            string
		expectedNodeAddresses []corev1.NodeAddress
		expectedError         error
	}{
		{"notkubevirt://instance", nil, fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
		{"kubevirt://missingVMI", nil, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{"kubevirt://nodeHostname", makeNodeAddressList("hostname", nil), nil},
		{"kubevirt://nodeNoHostname", makeNodeAddressList("", []string{"10.0.0.10"}), nil},
		{"kubevirt://nodeDomainHostname", makeNodeAddressList("node.domainname", []string{"10.0.0.11"}), nil},
		{"kubevirt://nodeDomainNoHostname", makeNodeAddressList("", []string{"10.0.0.12", "10.0.0.13", "10.0.0.14", "10.0.0.15", "10.0.0.16", "10.0.0.17"}), nil},
	}

	for _, test := range tests {
		nodeAddresses, err := i.NodeAddressesByProviderID(ctx, test.providerID)
		if !cmpNodeAddresses(nodeAddresses, test.expectedNodeAddresses) {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeAddresses, nodeAddresses)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseUnset", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseUnset),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhasePending", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhasePending),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseScheduling", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseScheduling),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseScheduled", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseScheduled),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseRunning", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseRunning),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseSucceeded", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "nodePhaseSucceeded")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseFailed", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "nodePhaseFailed")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseUnknown", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "nodePhaseUnknown")),
	)

	tests := []struct {
		nodeName           types.NodeName
		expectedInstanceID string
		expectedError      error
	}{
		{types.NodeName("missingVMI"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseUnset"), "nodePhaseUnset", nil},
		{types.NodeName("nodePhasePending"), "nodePhasePending", nil},
		{types.NodeName("nodePhaseScheduling"), "nodePhaseScheduling", nil},
		{types.NodeName("nodePhaseScheduled"), "nodePhaseScheduled", nil},
		{types.NodeName("nodePhaseRunning"), "nodePhaseRunning", nil},
		{types.NodeName("nodePhaseSucceeded"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseFailed"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseUnknown"), "", cloudprovider.InstanceNotFound},
	}

	for _, test := range tests {
		externalID, err := i.InstanceID(ctx, test.nodeName)
		if externalID != test.expectedInstanceID {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceID, externalID)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceType(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
		config: InstancesConfig{
			EnableInstanceTypes: true,
		},
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeFlavor", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeFlavor),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeNoFlavor", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeNoFlavor),
	)

	tests := []struct {
		nodeName             types.NodeName
		expectedInstanceType string
		expectedError        error
	}{
		{types.NodeName("missingVMI"), "", fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{types.NodeName("nodeFlavor"), "flavor", nil},
		{types.NodeName("nodeNoFlavor"), "", nil},
	}

	for _, test := range tests {
		instanceType, err := i.InstanceType(ctx, test.nodeName)
		if instanceType != test.expectedInstanceType {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceType, instanceType)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceTypeByProviderID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
		config: InstancesConfig{
			EnableInstanceTypes: true,
		},
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeFlavor", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeFlavor),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodeNoFlavor", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodeNoFlavor),
	)

	tests := []struct {
		providerID           string
		expectedInstanceType string
		expectedError        error
	}{
		{"notkubevirt://instance", "", fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
		{"kubevirt://missingVMI", "", fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{"kubevirt://nodeFlavor", "flavor", nil},
		{"kubevirt://nodeNoFlavor", "", nil},
	}

	for _, test := range tests {
		instanceType, err := i.InstanceTypeByProviderID(ctx, test.providerID)
		if instanceType != test.expectedInstanceType {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceType, instanceType)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestAddSSHKeyToAllInstances(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	// The kubevirt cloud provider does not implement the AddSSHKeyToAllInstances method
	err := i.AddSSHKeyToAllInstances(ctx, "user", []byte("keyData"))
	if err != cloudprovider.NotImplemented {
		t.Errorf("Expected: '%v', got '%v'", cloudprovider.NotImplemented, err)
	}
}

func TestCurrentNodeName(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiList),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiList),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiList),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiList),
		c.EXPECT().List(ctx, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstanceList{}), client.InNamespace("test")).SetArg(1, vmiList),
	)

	tests := []struct {
		hostname         string
		expectedNodeName types.NodeName
		expectedError    error
	}{
		{"missingVMI", "", cloudprovider.InstanceNotFound},
		{"hostname", types.NodeName("nodeHostname"), nil},
		{"nodeNoHostname", types.NodeName("nodeNoHostname"), nil},
		{"node.domainname", types.NodeName("nodeDomainHostname"), nil},
		{"nodeDomainNoHostname", types.NodeName("nodeDomainNoHostname"), nil},
	}

	for _, test := range tests {
		nodeName, err := i.CurrentNodeName(ctx, test.hostname)
		if nodeName != test.expectedNodeName {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeName, nodeName)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceExistsByProviderID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseRunning", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseRunning),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseSucceeded", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseSucceeded),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseFailed", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseFailed),
	)

	tests := []struct {
		providerID             string
		expectedInstanceExists bool
		expectedError          error
	}{
		{"kubevirt://missingVMI", false, nil},
		{"kubevirt://nodePhaseRunning", true, nil},
		{"kubevirt://nodePhaseSucceeded", true, nil},
		{"kubevirt://nodePhaseFailed", true, nil},
	}

	for _, test := range tests {
		instanceExists, err := i.InstanceExistsByProviderID(ctx, test.providerID)
		if instanceExists != test.expectedInstanceExists {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceExists, instanceExists)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceShutdownByProviderID(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	defer ctrl.Finish()
	c := mockclient.NewMockClient(ctrl)

	i := &instances{
		namespace: "test",
		client:    c,
	}

	gomock.InOrder(
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "missingVMI", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).Return(errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI")),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseUnset", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseUnset),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhasePending", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhasePending),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseScheduling", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseScheduling),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseScheduled", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseScheduled),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseRunning", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseRunning),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseSucceeded", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseSucceeded),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseFailed", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseFailed),
		c.EXPECT().Get(ctx, client.ObjectKey{Name: "nodePhaseUnknown", Namespace: "test"}, gomock.AssignableToTypeOf(&kubevirtv1.VirtualMachineInstance{})).SetArg(2, vmiNodePhaseUnknown),
	)

	tests := []struct {
		providerID               string
		expectedInstanceShutdown bool
		expectedError            error
	}{
		{"notkubevirt://instance", false, fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
		{"kubevirt://missingVMI", false, cloudprovider.InstanceNotFound},
		{"kubevirt://nodePhaseUnset", false, nil},
		{"kubevirt://nodePhasePending", false, nil},
		{"kubevirt://nodePhaseScheduling", false, nil},
		{"kubevirt://nodePhaseScheduled", false, nil},
		{"kubevirt://nodePhaseRunning", false, nil},
		{"kubevirt://nodePhaseSucceeded", true, nil},
		{"kubevirt://nodePhaseFailed", true, nil},
		{"kubevirt://nodePhaseUnknown", true, fmt.Errorf("Instance is in unkown state (propably host down)")},
	}

	for _, test := range tests {
		instanceShutdown, err := i.InstanceShutdownByProviderID(ctx, test.providerID)
		if instanceShutdown != test.expectedInstanceShutdown {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceShutdown, instanceShutdown)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}
