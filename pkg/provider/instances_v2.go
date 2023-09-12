package provider

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	config    *InstancesV2Config
}

// InstanceExists returns true if the instance for the given node exists according to the cloud provider.
func (i *instancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	_, err = InstanceByVMIName(instanceID).Get(ctx, i.client, i.namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Unable to find virtual machine instance %s", instanceID)
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the cloud provider.
func (i *instancesV2) InstanceShutdown(ctx context.Context, node *corev1.Node) (bool, error) {
	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	instance, err := InstanceByVMIName(instanceID).Get(ctx, i.client, i.namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	switch instance.Status.Phase {
	case kubevirtv1.Succeeded, kubevirtv1.Failed:
		klog.Infof("instance %s is shut down.", instance.Name)
		return true, nil
	case kubevirtv1.Unknown:
		return true, fmt.Errorf("Instance is in unkown state (propably host down)")
	default:
		return false, nil
	}
}

// InstanceMetadata returns the instance's metadata.
func (i *instancesV2) InstanceMetadata(ctx context.Context, node *corev1.Node) (*cloudprovider.InstanceMetadata, error) {
	var instanceGetters []InstanceGetter

	if val, ok := node.Labels[instanceIDLabelKey]; ok {
		instanceGetters = append(instanceGetters, InstanceByVMIName(val))
	} else {
		instanceGetters = append(instanceGetters, InstanceByVMIName(node.Name), InstanceByVMIHostname(node.Name))
	}

	instance, err := i.findInstance(ctx, instanceGetters...)
	if err != nil {
		return nil, err
	}
	addrs := i.getNodeAddresses(instance.Status.Interfaces, node.Status.Addresses)

	instanceType := ""
	if val, ok := instance.Annotations[kubevirtv1.InstancetypeAnnotation]; ok {
		instanceType = val
	}

	region, zone, err := i.getRegionAndZone(ctx, instance.Status.NodeName)
	if err != nil {
		return nil, err
	}

	return &cloudprovider.InstanceMetadata{
		ProviderID:    getProviderID(instance.Name),
		NodeAddresses: addrs,
		InstanceType:  instanceType,
		Region:        region,
		Zone:          zone,
	}, nil
}

// findInstance finds a virtual machine instance of the corresponding node
func (i *instancesV2) findInstance(ctx context.Context, fetchers ...InstanceGetter) (*kubevirtv1.VirtualMachineInstance, error) {
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

func (i *instancesV2) getNodeAddresses(ifs []kubevirtv1.VirtualMachineInstanceNetworkInterface, prevAddrs []corev1.NodeAddress) []corev1.NodeAddress {
	var addrs []corev1.NodeAddress

	foundInternalIP := false
	// TODO: detect type of all addresses, right now pick only the default
	for _, i := range ifs {
		// Only change the IP if it is known, not if it is empty
		if i.Name == "default" && i.IP != "" {
			v1helper.AddToNodeAddresses(&addrs, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: i.IP,
			})
			foundInternalIP = true
			break
		}
	}

	// fall back to the previously known internal IP on the node
	// if the default IP on the vmi.status.interfaces is not present.
	// This smooths over issues where the vmi.status.interfaces field is
	// not reporting results due to an internal reboot or other issues when
	// contacting the qemu guest agent.
	if !foundInternalIP {
		for _, prevAddr := range prevAddrs {
			if prevAddr.Type == corev1.NodeInternalIP {
				v1helper.AddToNodeAddresses(&addrs, prevAddr)
			}
		}
	}

	return addrs
}

func (i *instancesV2) getRegionAndZone(ctx context.Context, nodeName string) (string, string, error) {
	region, zone := "", ""
	if i.config != nil && !i.config.ZoneAndRegionEnabled {
		return region, zone, nil
	}

	node := corev1.Node{}

	err := i.client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
	if err != nil {
		return "", "", err
	}

	if val, ok := node.Labels[corev1.LabelTopologyRegion]; ok {
		region = val
	}
	if val, ok := node.Labels[corev1.LabelTopologyZone]; ok {
		zone = val
	}

	return region, zone, nil
}

func getProviderID(instanceID string) string {
	return fmt.Sprintf("%s://%s", ProviderName, instanceID)
}

// instanceIDFromProviderID extracts the instance ID from a provider ID.
func instanceIDFromProviderID(providerID string) (instanceID string, err error) {
	matches := providerIDRegexp.FindStringSubmatch(providerID)
	if len(matches) != 2 {
		return "", fmt.Errorf("ProviderID \"%s\" didn't match expected format \"%s://<instance-id>\"", providerID, ProviderName)
	}
	return matches[1], nil
}
