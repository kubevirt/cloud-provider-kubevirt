package kubevirt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/cloudprovider"
)

const (
	// Prefix of the service label to put on VMIs
	serviceVmiLabelKeyPrefix = "service.kubevirt.io"
	// Interval in seconds between polling the service after creation
	loadBalancerCreatePollIntervalSeconds = 5
)

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *cloud) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	lbName := cloudprovider.GetLoadBalancerName(service)
	lbService, serviceExists, err := c.getLoadBalancerService(lbName)
	if err != nil {
		glog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, false, err
	}
	if !serviceExists {
		return nil, false, nil
	}

	status = &lbService.Status.LoadBalancer
	return status, true, nil
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *cloud) EnsureLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	lbName := cloudprovider.GetLoadBalancerName(service)

	err := c.applyServiceLabelsToVmis(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		glog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return nil, err
	}

	lbService, lbExists, err := c.getLoadBalancerService(lbName)
	if err != nil {
		glog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, err
	}
	if lbExists {
		return &lbService.Status.LoadBalancer, nil
	}

	lbService, err = c.createLoadBalancerService(lbName, service)
	if err != nil {
		glog.Errorf("Failed to create LoadBalancer service: %v", err)
		return nil, err
	}

	err = wait.PollUntil(loadBalancerCreatePollIntervalSeconds*time.Second, func() (bool, error) {
		if len(lbService.Status.LoadBalancer.Ingress) != 0 {
			return true, nil
		}
		service, exists, err := c.getLoadBalancerService(lbName)
		if err != nil {
			glog.Errorf("Failed to get LoadBalancer service: %v", err)
			return false, err
		}
		if exists && len(service.Status.LoadBalancer.Ingress) > 0 {
			lbService = service
			return true, nil
		}
		return false, nil
	}, ctx.Done())
	if err != nil {
		glog.Errorf("Failed to poll LoadBalancer service: %v", err)
		return nil, err
	}

	return &lbService.Status.LoadBalancer, nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *cloud) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	lbName := cloudprovider.GetLoadBalancerName(service)
	err := c.applyServiceLabelsToVmis(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		glog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return err
	}
	err = c.ensureVmiServiceLabelsDeleted(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		glog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return err
	}
	return nil
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
// This construction is useful because many cloud providers' load balancers
// have multiple underlying components, meaning a Get could say that the LB
// doesn't exist even if some part of it is still laying around.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *cloud) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	lbName := cloudprovider.GetLoadBalancerName(service)

	lbService, lbExists, err := c.getLoadBalancerService(lbName)
	if err != nil {
		glog.Errorf("Failed to get LoadBalancer service: %v", err)
		return err
	}
	if lbExists {
		err = c.kubernetes.CoreV1().Services(c.namespace).Delete(lbService.ObjectMeta.Name, &metav1.DeleteOptions{})
		if err != nil {
			glog.Errorf("Failed to delete LoadBalancer service: %v", err)
			return err
		}
	}

	err = c.ensureVmiServiceLabelsDeleted(lbName, service.ObjectMeta.Name, []*corev1.Node{})
	if err != nil {
		glog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return err
	}

	return nil
}

func (c *cloud) getLoadBalancerService(lbName string) (*corev1.Service, bool, error) {
	service, err := c.kubernetes.CoreV1().Services(c.namespace).Get(lbName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return service, true, nil
}

func (c *cloud) createLoadBalancerService(lbName string, service *corev1.Service) (*corev1.Service, error) {
	ports := make([]corev1.ServicePort, len(service.Spec.Ports))
	for i, port := range service.Spec.Ports {
		ports[i].Name = port.Name
		ports[i].Protocol = port.Protocol
		ports[i].Port = port.Port
		ports[i].TargetPort = intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: port.NodePort,
		}
	}

	lbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbName,
			Namespace: c.namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: map[string]string{serviceVmiLabelKey(lbName): service.ObjectMeta.Name},
			Type:     corev1.ServiceTypeLoadBalancer,
		},
	}
	if len(service.Spec.ExternalIPs) > 0 {
		lbService.Spec.ExternalIPs = service.Spec.ExternalIPs
	}
	if service.Spec.LoadBalancerIP != "" {
		lbService.Spec.LoadBalancerIP = service.Spec.LoadBalancerIP
	}
	if service.Spec.HealthCheckNodePort > 0 {
		lbService.Spec.HealthCheckNodePort = service.Spec.HealthCheckNodePort
	}

	lbService, err := c.kubernetes.CoreV1().Services(c.namespace).Create(lbService)
	if err != nil {
		glog.Errorf("Failed to create LB %s: %v", lbName, err)
		return nil, err
	}
	return lbService, nil
}

func (c *cloud) applyServiceLabelsToVmis(lbName, serviceName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	allVmis, err := c.kubevirt.VirtualMachineInstance(c.namespace).List(&metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}

	// Apply labels to all VMIs for the service to match
	for _, vmi := range allVmis.Items {
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; ok {
			vmi.ObjectMeta.Labels[serviceVmiLabelKey(lbName)] = serviceName
			_, err = c.kubevirt.VirtualMachineInstance(c.namespace).Update(&vmi)
			if err != nil {
				return fmt.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			}
		}
	}
	return nil
}

func (c *cloud) ensureVmiServiceLabelsDeleted(lbName, svcName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	listOptions := &metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", serviceVmiLabelKey(lbName), svcName),
	}
	vmis, err := c.kubevirt.VirtualMachineInstance(c.namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	for _, vmi := range vmis.Items {
		// Delete labels on those VMIs which do not have a corresponding node object
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; !ok {
			delete(vmi.ObjectMeta.Labels, serviceVmiLabelKey(lbName))
			_, err = c.kubevirt.VirtualMachineInstance(c.namespace).Update(&vmi)
			if err != nil {
				return fmt.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			}
		}
	}
	return nil
}

func buildInstanceIDMap(nodes []*corev1.Node) map[string]struct{} {
	instanceIDs := make(map[string]struct{}, len(nodes))
	for _, instanceID := range instanceIDsFromNodes(nodes) {
		instanceIDs[instanceID] = struct{}{}
	}
	return instanceIDs
}

func serviceVmiLabelKey(lbName string) string {
	return strings.Join([]string{serviceVmiLabelKeyPrefix, lbName}, "/")
}
