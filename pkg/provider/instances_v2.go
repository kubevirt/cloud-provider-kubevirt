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
	config    *InstancesV2Config
}

// InstanceExists returns true if the instance for the given node exists according to the cloud provider.
func (i *instancesV2) InstanceExists(ctx context.Context, node *corev1.Node) (bool, error) {
	instanceID, err := instanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		return false, err
	}

	instance, err := InstanceByVMIName(instanceID).Get(ctx, i.client, i.namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("Unable to find virtual machine instance %s", instanceID)
			return false, nil
		}
		return false, err
	}

	switch instance.Status.Phase {
	case kubevirtv1.Succeeded, kubevirtv1.Failed:
		recoverable, err := i.isInstanceRecoverable(ctx, instance)
		if err != nil {
			return false, err
		}
		return recoverable, nil
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
		return true, fmt.Errorf("instance is in unkown state (propably host down)")
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
	addrs := i.getNodeAddresses(instance.Status.Interfaces)

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

// isInstanceRecoverable checks if VM is in a finalized phase and should remain stopped.
// If the VM should remain stopped we must allow the corresponding node object to be deleted as it cannot leave the NotReady state anymore.
// The CCM does not clean up left VMs, it is up to the user or dedicated controller to do so.
func (i *instancesV2) isInstanceRecoverable(ctx context.Context, instance *kubevirtv1.VirtualMachineInstance) (bool, error) {
	vm := kubevirtv1.VirtualMachine{}

	err := i.client.Get(ctx, types.NamespacedName{Namespace: i.namespace, Name: instance.Name}, &vm)
	if err != nil {
		return false, err
	}

	if vm.Spec.RunStrategy != nil && *vm.Spec.RunStrategy == kubevirtv1.RunStrategyOnce {
		klog.Infof("Found VMI in %q phase with %q run strategy - reporting instance %q as not found",
			instance.Status.Phase, kubevirtv1.RunStrategyOnce, instance.Name)
		return false, nil
	}

	return true, nil
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
