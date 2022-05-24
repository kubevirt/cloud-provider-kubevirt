package provider

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InstanceFetcher allows fetching virtual machine instances with multiple fetching strategies
type InstanceFetcher interface {
	// Get gets a virtual machine instance
	Get(ctx context.Context, cli client.Client, namespace string) (*kubevirtv1.VirtualMachineInstance, error)
}

// InstanceByVMIName tries to fetch a vmi by its name
type InstanceByVMIName string

func (i InstanceByVMIName) Get(ctx context.Context, cli client.Client, namespace string) (*kubevirtv1.VirtualMachineInstance, error) {
	var instance kubevirtv1.VirtualMachineInstance

	err := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: string(i)}, &instance)
	if err != nil {
		return nil, err
	}
	return &instance, nil
}

// InstanceByVMIHostname tries to fetch a vmi by its hostname
type InstanceByVMIHostname string

func (i InstanceByVMIHostname) Get(ctx context.Context, cli client.Client, namespace string) (*kubevirtv1.VirtualMachineInstance, error) {
	var instances kubevirtv1.VirtualMachineInstanceList
	hostname := string(i)

	lo := &client.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.SelectorFromSet(fields.Set{"spec.hostname": hostname}),
	}
	err := cli.List(ctx, &instances, lo)
	if err != nil {
		return nil, err
	}

	instancesNum := len(instances.Items)
	if instancesNum > 1 {
		return nil, fmt.Errorf("found %d virtual machine instances with the same '%s' hostname",
			instancesNum, hostname)
	} else if instancesNum < 1 {
		return nil, fmt.Errorf("could not find a virtual machine instance with '%s' hostname", hostname)
	}
	return &instances.Items[0], nil
}
