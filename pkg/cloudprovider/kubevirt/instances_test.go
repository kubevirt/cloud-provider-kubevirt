package kubevirt

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/cloudprovider"
	kubevirtv1 "kubevirt.io/kubevirt/pkg/api/v1"
	"kubevirt.io/kubevirt/pkg/kubecli"
)

func mockInstances(t *testing.T, namespace string) cloudprovider.Instances {
	ctrl := gomock.NewController(t)
	kubevirt := kubecli.NewMockKubevirtClient(ctrl)

	vmiInterface := kubecli.NewMockVirtualMachineInstanceInterface(ctrl)
	kubevirt.EXPECT().VirtualMachineInstance(gomock.Eq(namespace)).Return(vmiInterface).AnyTimes()

	vmiMap := map[string]*kubevirtv1.VirtualMachineInstance{
		"nodeHostname":         kubevirtv1.NewMinimalVMIWithNS(namespace, "nodehostname"),
		"nodeNoHostname":       kubevirtv1.NewMinimalVMIWithNS(namespace, "nodenohostname"),
		"nodeDomainHostname":   kubevirtv1.NewMinimalVMIWithNS(namespace, "node.domainhostname"),
		"nodeDomainNoHostname": kubevirtv1.NewMinimalVMIWithNS(namespace, "node.domainnohostname"),
		"nodePhaseUnset":       kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephaseunset"),
		"nodePhasePending":     kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephasepending"),
		"nodePhaseScheduling":  kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephasescheduling"),
		"nodePhaseScheduled":   kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephasescheduled"),
		"nodePhaseRunning":     kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephaserunning"),
		"nodePhaseSucceeded":   kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephasesucceeded"),
		"nodePhaseFailed":      kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephasefailed"),
		"nodePhaseUnknown":     kubevirtv1.NewMinimalVMIWithNS(namespace, "nodephaseunknown"),
		"nodeFlavor":           kubevirtv1.NewMinimalVMIWithNS(namespace, "nodeflavor"),
		"nodeNoFlavor":         kubevirtv1.NewMinimalVMIWithNS(namespace, "nodenoflavor"),
	}

	vmiMap["nodeHostname"].Spec.Hostname = "hostname"
	vmiMap["nodeDomainHostname"].Spec.Hostname = "node.domainname"

	vmiMap["nodeNoHostname"].Status.Interfaces = []kubevirtv1.VirtualMachineInstanceNetworkInterface{
		kubevirtv1.VirtualMachineInstanceNetworkInterface{
			IP: "10.0.0.10",
		},
	}
	vmiMap["nodeDomainHostname"].Status.Interfaces = []kubevirtv1.VirtualMachineInstanceNetworkInterface{
		kubevirtv1.VirtualMachineInstanceNetworkInterface{
			IPs: []string{"10.0.0.11"},
		},
	}
	vmiMap["nodeDomainNoHostname"].Status.Interfaces = []kubevirtv1.VirtualMachineInstanceNetworkInterface{
		kubevirtv1.VirtualMachineInstanceNetworkInterface{
			IP:  "10.0.0.12",
			IPs: []string{"10.0.0.13"},
		},
		kubevirtv1.VirtualMachineInstanceNetworkInterface{
			IPs: []string{"10.0.0.14", "10.0.0.15"},
		},
		kubevirtv1.VirtualMachineInstanceNetworkInterface{
			IP:  "10.0.0.16",
			IPs: []string{"10.0.0.16", "10.0.0.17"},
		},
	}

	vmiMap["nodePhaseUnset"].Status.Phase = ""
	vmiMap["nodePhasePending"].Status.Phase = "Pending"
	vmiMap["nodePhaseScheduling"].Status.Phase = "Scheduling"
	vmiMap["nodePhaseScheduled"].Status.Phase = "Scheduled"
	vmiMap["nodePhaseRunning"].Status.Phase = "Running"
	vmiMap["nodePhaseSucceeded"].Status.Phase = "Succeeded"
	vmiMap["nodePhaseFailed"].Status.Phase = "Failed"
	vmiMap["nodePhaseUnknown"].Status.Phase = "Unknown"

	vmiMap["nodeFlavor"].ObjectMeta.Annotations = map[string]string{
		"cloud.kubevirt.io/flavor": "flavor",
	}

	vmiListItems := make([]kubevirtv1.VirtualMachineInstance, 0, len(vmiMap))
	for name, vmi := range vmiMap {
		vmiListItems = append(vmiListItems, *vmi)
		vmiInterface.EXPECT().Get(gomock.Eq(name), gomock.Any()).Return(vmi, nil)
		//vmiInterface.EXPECT().Get(gomock.Eq(vmi.ObjectMeta.Name), gomock.Any()).Return(vmi, nil)
	}
	vmiInterface.EXPECT().Get(gomock.Eq("missingVMI"), gomock.Any()).Return(nil, errors.NewNotFound(schema.GroupResource{Group: "kubevirt.io", Resource: "virtualmachineinstances"}, "missingVMI"))
	vmiList := kubevirtv1.VirtualMachineInstanceList{
		Items: vmiListItems,
	}
	vmiInterface.EXPECT().List(gomock.Any()).Return(&vmiList, nil).AnyTimes()

	return &instances{
		namespace: namespace,
		kubevirt:  kubevirt,
	}
}

func makeNodeAddressList(hostname string, internalIPs []string) []corev1.NodeAddress {
	addresses := make([]corev1.NodeAddress, 0)
	addresses = append(addresses, corev1.NodeAddress{
		Type:    corev1.NodeHostName,
		Address: hostname,
	})
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
	i := mockInstances(t, "testNodeAddresses")
	tests := []struct {
		nodeName              types.NodeName
		expectedNodeAddresses []corev1.NodeAddress
		expectedError         error
	}{
		{types.NodeName("missingVMI"), nil, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{types.NodeName("nodeHostname"), makeNodeAddressList("hostname", nil), nil},
		{types.NodeName("nodeNoHostname"), makeNodeAddressList("nodenohostname", []string{"10.0.0.10"}), nil},
		{types.NodeName("nodeDomainHostname"), makeNodeAddressList("node.domainname", []string{"10.0.0.11"}), nil},
		{types.NodeName("nodeDomainNoHostname"), makeNodeAddressList("node.domainnohostname", []string{"10.0.0.12", "10.0.0.13", "10.0.0.14", "10.0.0.15", "10.0.0.16", "10.0.0.17"}), nil},
	}

	for _, test := range tests {
		nodeAddresses, err := i.NodeAddresses(context.TODO(), test.nodeName)
		if !cmpNodeAddresses(nodeAddresses, test.expectedNodeAddresses) {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeAddresses, nodeAddresses)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestNodeAddressesByProviderID(t *testing.T) {
	i := mockInstances(t, "testNodeAddressesByProviderID")
	tests := []struct {
		providerID            string
		expectedNodeAddresses []corev1.NodeAddress
		expectedError         error
	}{
		{"notkubevirt://instance", nil, fmt.Errorf("ProviderID \"notkubevirt://instance\" didn't match expected format \"kubevirt://InstanceID\"")},
		{"kubevirt://missingVMI", nil, fmt.Errorf("virtualmachineinstances.kubevirt.io \"missingVMI\" not found")},
		{"kubevirt://nodeHostname", makeNodeAddressList("hostname", nil), nil},
		{"kubevirt://nodeNoHostname", makeNodeAddressList("nodenohostname", []string{"10.0.0.10"}), nil},
		{"kubevirt://nodeDomainHostname", makeNodeAddressList("node.domainname", []string{"10.0.0.11"}), nil},
		{"kubevirt://nodeDomainNoHostname", makeNodeAddressList("node.domainnohostname", []string{"10.0.0.12", "10.0.0.13", "10.0.0.14", "10.0.0.15", "10.0.0.16", "10.0.0.17"}), nil},
	}

	for _, test := range tests {
		nodeAddresses, err := i.NodeAddressesByProviderID(context.TODO(), test.providerID)
		if !cmpNodeAddresses(nodeAddresses, test.expectedNodeAddresses) {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeAddresses, nodeAddresses)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceID(t *testing.T) {
	i := mockInstances(t, "testInstanceID")
	tests := []struct {
		nodeName           types.NodeName
		expectedInstanceID string
		expectedError      error
	}{
		{types.NodeName("missingVMI"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseUnset"), "nodephaseunset", nil},
		{types.NodeName("nodePhasePending"), "nodephasepending", nil},
		{types.NodeName("nodePhaseScheduling"), "nodephasescheduling", nil},
		{types.NodeName("nodePhaseScheduled"), "nodephasescheduled", nil},
		{types.NodeName("nodePhaseRunning"), "nodephaserunning", nil},
		{types.NodeName("nodePhaseSucceeded"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseFailed"), "", cloudprovider.InstanceNotFound},
		{types.NodeName("nodePhaseUnknown"), "", cloudprovider.InstanceNotFound},
	}

	for _, test := range tests {
		externalID, err := i.InstanceID(context.TODO(), test.nodeName)
		if externalID != test.expectedInstanceID {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceID, externalID)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceType(t *testing.T) {
	i := mockInstances(t, "testInstanceType")
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
		instanceType, err := i.InstanceType(context.TODO(), test.nodeName)
		if instanceType != test.expectedInstanceType {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceType, instanceType)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceTypeByProviderID(t *testing.T) {
	i := mockInstances(t, "testInstanceTypeByProviderID")
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
		instanceType, err := i.InstanceTypeByProviderID(context.TODO(), test.providerID)
		if instanceType != test.expectedInstanceType {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceType, instanceType)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestAddSSHKeyToAllInstances(t *testing.T) {
	// The kubevirt cloud provider does not implement the AddSSHKeyToAllInstances method
	i := mockInstances(t, "testAddSSHKeyToAllInstances")
	err := i.AddSSHKeyToAllInstances(context.TODO(), "user", []byte("keyData"))
	if err != cloudprovider.NotImplemented {
		t.Errorf("Expected: '%v', got '%v'", cloudprovider.NotImplemented, err)
	}
}

func TestCurrentNodeName(t *testing.T) {
	i := mockInstances(t, "testCurrentNodeName")
	tests := []struct {
		hostname         string
		expectedNodeName types.NodeName
		expectedError    error
	}{
		{"missingVMI", "", cloudprovider.InstanceNotFound},
		{"hostname", types.NodeName("nodehostname"), nil},
		{"nodenohostname", types.NodeName("nodenohostname"), nil},
		{"node.domainname", types.NodeName("node.domainhostname"), nil},
		{"node.domainnohostname", types.NodeName("node.domainnohostname"), nil},
	}

	for _, test := range tests {
		nodeName, err := i.CurrentNodeName(context.TODO(), test.hostname)
		if nodeName != test.expectedNodeName {
			t.Errorf("Expected: %v, got: %v", test.expectedNodeName, nodeName)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceExistsByProviderID(t *testing.T) {
	i := mockInstances(t, "testInstanceExistsByProviderID")
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
		instanceExists, err := i.InstanceExistsByProviderID(context.TODO(), test.providerID)
		if instanceExists != test.expectedInstanceExists {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceExists, instanceExists)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestInstanceShutdownByProviderID(t *testing.T) {
	i := mockInstances(t, "testInstanceShutdownByProviderID")
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
		instanceShutdown, err := i.InstanceShutdownByProviderID(context.TODO(), test.providerID)
		if instanceShutdown != test.expectedInstanceShutdown {
			t.Errorf("Expected: %v, got: %v", test.expectedInstanceShutdown, instanceShutdown)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}
