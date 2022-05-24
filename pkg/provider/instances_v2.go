package provider

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	v1helper "k8s.io/cloud-provider/node/helpers"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// instanceIDLabelKey could be injected by k8s providers to find a corresponding virtual machine instance
	// the value should be a virtual machine name
	instanceIDLabelKey = "node.kubernetes.io/instance-id"
)

// Must match providerIDs built by cloudprovider.GetInstanceProviderID
var providerIDRegexp = regexp.MustCompile(`^` + ProviderName + `://([0-9A-Za-z_-]+)$`)

type instancesV2 struct {
	namespace string
	client    client.Client
	config    InstancesV2Config
}

// InstanceExists returns true if the instance for the given node exists according to the cloud provider.
func (i *instancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	err = i.client.Get(ctx, types.NamespacedName{Name: instanceID, Namespace: i.namespace}, nil)
	if err != nil {
		return false, err
	}

	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the cloud provider.
func (i *instancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	var instance kubevirtv1.VirtualMachineInstance

	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	err = i.client.Get(ctx, types.NamespacedName{Name: instanceID, Namespace: i.namespace}, &instance)
	if err != nil {
		return false, err
	}

	switch instance.Status.Phase {
	case kubevirtv1.Succeeded, kubevirtv1.Failed:
		klog.Infof("instance %s is shut down.", instance.Name)
		return true, nil
	case kubevirtv1.Unknown:
		klog.Infof("instance %s is in an unknown state (host probably down).", instance.Name)
		return true, nil
	default:
		return false, nil
	}
}

// InstanceMetadata returns the instance's metadata.
func (i *instancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	var instanceFetchers []InstanceFetcher

	if val, ok := node.Labels[instanceIDLabelKey]; ok {
		instanceFetchers = append(instanceFetchers, InstanceByVMIName(val))
	} else {
		instanceFetchers = append(instanceFetchers, InstanceByVMIName(node.Name), InstanceByVMIHostname(node.Name))
	}

	instance, err := i.getInstance(ctx, instanceFetchers...)
	if err != nil {
		return nil, err
	}
	addrs := i.getNodeAddresses(instance.Status.Interfaces)

	instanceType := ""
	if val, ok := instance.Annotations[kubevirtv1.FlavorAnnotation]; ok {
		instanceType = val
	}

	region := ""
	if val, ok := instance.Labels[corev1.LabelTopologyRegion]; ok {
		region = val
	}

	zone := ""
	if val, ok := instance.Labels[corev1.LabelTopologyZone]; ok {
		zone = val
	}

	return &cloudprovider.InstanceMetadata{
		ProviderID:    getProviderID(instance.Name),
		NodeAddresses: addrs,
		InstanceType:  instanceType,
		Region:        region,
		Zone:          zone,
	}, nil
}

// getInstance finds a virtual machine instance of the corresponding node
func (i *instancesV2) getInstance(ctx context.Context, fetchers ...InstanceFetcher) (*kubevirtv1.VirtualMachineInstance, error) {
	var (
		instance *kubevirtv1.VirtualMachineInstance
		err      error
	)

	for _, f := range fetchers {
		instance, err = f.Get(ctx, i.client, i.namespace)
		if err != nil && !errors.IsNotFound(err) {
			return nil, err
		} else if instance != nil {
			break
		}
	}

	if instance == nil {
		return nil, cloudprovider.InstanceNotFound
	}

	return instance, nil
}

func (i *instancesV2) getNodeAddresses(ifs []kubevirtv1.VirtualMachineInstanceNetworkInterface) []corev1.NodeAddress {
	var addrs []corev1.NodeAddress

	// TODO: detect type of all addresses, right now pick only the default
	for _, i := range ifs {
		if i.Name == "default" {
			v1helper.AddToNodeAddresses(&addrs, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: i.IP,
			})
			break
		}
	}

	return addrs
}

func getProviderID(instanceID string) string {
	return fmt.Sprintf("%s://%s", ProviderName, instanceID)
}

func instanceIDsFromNodes(nodes []*corev1.Node) []string {
	instanceIDs := make([]string, len(nodes))
	for i, node := range nodes {
		if instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID); err == nil {
			instanceIDs[i] = instanceID
		}
	}
	return instanceIDs
}

// instanceIDFromProviderID extracts the instance ID from a provider ID.
func instanceIDFromProviderID(providerID string) (instanceID string, err error) {
	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 2 {
		return "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"%s://<instance-id>\"", providerID, ProviderName)
	}
	return matches[1], nil
}
