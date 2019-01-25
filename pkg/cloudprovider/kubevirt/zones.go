package kubevirt

import (
	"context"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/cloudprovider"
	kubeletapis "k8s.io/kubernetes/pkg/kubelet/apis"
	"kubevirt.io/kubevirt/pkg/kubecli"
)

type zones struct {
	namespace  string
	kubernetes kubernetes.Clientset
	kubevirt   kubecli.KubevirtClient
}

// GetZone returns the Zone containing the current failure zone and locality region that the program is running in
// In most cases, this method is called from the kubelet querying a local metadata service to acquire its zone.
// For the case of external cloud providers, use GetZoneByProviderID or GetZoneByNodeName since GetZone
// can no longer be called from the kubelets.
func (z *zones) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{}, cloudprovider.NotImplemented
}

// GetZoneByProviderID returns the Zone containing the current zone and locality region of the node specified by providerId
// This method is particularly used in the context of external cloud providers where node initialization must be down
// outside the kubelets.
func (z *zones) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	instanceID, err := instanceIDFromProviderID(providerID)
	if err != nil {
		glog.Errorf("Failed to get instance with provider ID %s in namespace %s: %v", providerID, z.namespace, err)
		return cloudprovider.Zone{}, err
	}
	return z.getZoneByInstanceID(ctx, instanceID)
}

// GetZoneByNodeName returns the Zone containing the current zone and locality region of the node specified by node name
// This method is particularly used in the context of external cloud providers where node initialization must be down
// outside the kubelets.
func (z *zones) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	instanceID := instanceIDFromNodeName(string(nodeName))
	return z.getZoneByInstanceID(ctx, instanceID)
}

func (z *zones) getZoneByInstanceID(ctx context.Context, instanceID string) (cloudprovider.Zone, error) {
	vmi, err := z.kubevirt.VirtualMachineInstance(z.namespace).Get(instanceID, &metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get instance with name %s in namespace %s: %v", instanceID, z.namespace, err)
		return cloudprovider.Zone{}, err
	}

	nodeName := vmi.Status.NodeName
	node, err := z.kubernetes.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get node %s: %v", nodeName, err)
		return cloudprovider.Zone{}, err
	}

	// Take over failure domain and region from node where the instance is running on.
	zone := cloudprovider.Zone{}
	if failureDomain, ok := node.ObjectMeta.Labels[kubeletapis.LabelZoneFailureDomain]; ok {
		zone.FailureDomain = failureDomain
	}
	if region, ok := node.ObjectMeta.Labels[kubeletapis.LabelZoneRegion]; ok {
		zone.Region = region
	}

	return zone, nil
}
