package kubevirt

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog"
	"kubevirt.io/client-go/kubecli"
)

const (
	// Prefix of the service label to put on VMIs and pods
	serviceLabelKeyPrefix = "cloud.kubevirt.io"
	// Default interval in seconds between polling the service after creation
	defaultLoadBalancerCreatePollInterval = 5
)

type loadbalancer struct {
	namespace string
	kubevirt  kubecli.KubevirtClient
	config    LoadBalancerConfig
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	lbService, serviceExists, err := lb.getLoadBalancerService(lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, false, err
	}
	if !serviceExists {
		return nil, false, nil
	}

	status = &lbService.Status.LoadBalancer
	return status, true, nil
}

// GetLoadBalancerName is an implementation of LoadBalancer.GetLoadBalancerName.
func (lb *loadbalancer) GetLoadBalancerName(ctx context.Context, clusterName string, service *corev1.Service) string {
	// TODO: replace DefaultLoadBalancerName to generate more meaningful loadbalancer names.
	return cloudprovider.DefaultLoadBalancerName(service)
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) EnsureLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) (*corev1.LoadBalancerStatus, error) {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)

	err := lb.applyServiceLabels(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return nil, err
	}
	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return nil, err
	}

	lbService, lbExists, err := lb.getLoadBalancerService(lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, err
	}
	if lbExists {
		if !equality.Semantic.DeepEqual(service.Spec.Ports, lbService.Spec.Ports) {
			lbService.Spec.Ports = lb.createLoadBalancerServicePorts(service)
			lb.kubevirt.CoreV1().Services(lb.namespace).Update(lbService)
		}
		return &lbService.Status.LoadBalancer, nil
	}

	lbService, err = lb.createLoadBalancerService(lbName, service)
	if err != nil {
		klog.Errorf("Failed to create LoadBalancer service: %v", err)
		return nil, err
	}

	err = wait.PollUntil(lb.getLoadBalancerCreatePollInterval()*time.Second, func() (bool, error) {
		if len(lbService.Status.LoadBalancer.Ingress) != 0 {
			return true, nil
		}
		service, exists, err := lb.getLoadBalancerService(lbName)
		if err != nil {
			klog.Errorf("Failed to get LoadBalancer service: %v", err)
			return false, err
		}
		if exists && len(service.Status.LoadBalancer.Ingress) > 0 {
			lbService = service
			return true, nil
		}
		return false, nil
	}, ctx.Done())
	if err != nil {
		klog.Errorf("Failed to poll LoadBalancer service: %v", err)
		return nil, err
	}

	return &lbService.Status.LoadBalancer, nil
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	err := lb.applyServiceLabels(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return err
	}
	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
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
func (lb *loadbalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)

	lbService, lbExists, err := lb.getLoadBalancerService(lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return err
	}
	if lbExists {
		err = lb.kubevirt.CoreV1().Services(lb.namespace).Delete(lbService.ObjectMeta.Name, &metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("Failed to delete LoadBalancer service: %v", err)
			return err
		}
	}

	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, []*corev1.Node{})
	if err != nil {
		klog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return err
	}

	return nil
}

func (lb *loadbalancer) getLoadBalancerService(lbName string) (*corev1.Service, bool, error) {
	service, err := lb.kubevirt.CoreV1().Services(lb.namespace).Get(lbName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return service, true, nil
}

func (lb *loadbalancer) createLoadBalancerService(lbName string, service *corev1.Service) (*corev1.Service, error) {
	ports := lb.createLoadBalancerServicePorts(service)
	lbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbName,
			Namespace: lb.namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: map[string]string{serviceLabelKey(lbName): service.ObjectMeta.Name},
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

	lbService, err := lb.kubevirt.CoreV1().Services(lb.namespace).Create(lbService)
	if err != nil {
		klog.Errorf("Failed to create LB %s: %v", lbName, err)
		return nil, err
	}
	return lbService, nil
}

func (lb *loadbalancer) createLoadBalancerServicePorts(service *corev1.Service) []corev1.ServicePort {
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
	return ports
}

func (lb *loadbalancer) applyServiceLabels(lbName, serviceName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	allVmis, err := lb.kubevirt.VirtualMachineInstance(lb.namespace).List(&metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	vmiUIDs := make([]string, len(instanceIDs))

	// Apply labels to all VMIs for the service to match
	for _, vmi := range allVmis.Items {
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; ok {
			vmi.ObjectMeta.Labels[serviceLabelKey(lbName)] = serviceName
			_, err = lb.kubevirt.VirtualMachineInstance(lb.namespace).Update(&vmi)
			if err != nil {
				klog.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			} else {
				// Remember updated VMI UIDs to find the correspomding pods
				vmiUIDs = append(vmiUIDs, string(vmi.ObjectMeta.UID))
			}
		}
	}

	// Find all pods created by a VMI
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubevirt.io/created-by in (%s)", strings.Join(vmiUIDs, ", ")),
	}
	vmiPods, err := lb.kubevirt.CoreV1().Pods(lb.namespace).List(listOptions)

	// Apply labels to all found pods
	for _, pod := range vmiPods.Items {
		pod.ObjectMeta.Labels[serviceLabelKey(lbName)] = serviceName
		_, err = lb.kubevirt.CoreV1().Pods(lb.namespace).Update(&pod)
		if err != nil {
			klog.Errorf("Failed to update pod %s: %v", pod.ObjectMeta.Name, err)
		}
	}
	return nil
}

func (lb *loadbalancer) ensureServiceLabelsDeleted(lbName, svcName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", serviceLabelKey(lbName), svcName),
	}
	vmis, err := lb.kubevirt.VirtualMachineInstance(lb.namespace).List(&listOptions)
	if err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	pods, err := lb.kubevirt.CoreV1().Pods(lb.namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Failed to list pods: %v", err)
	}
	vmiUIDs := make(map[string]struct{}, len(instanceIDs))

	// Delete labels on those VMIs & pods which do not have a corresponding node object
	for _, vmi := range vmis.Items {
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; !ok {
			delete(vmi.ObjectMeta.Labels, serviceLabelKey(lbName))
			_, err = lb.kubevirt.VirtualMachineInstance(lb.namespace).Update(&vmi)
			if err != nil {
				return fmt.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			}
			vmiUIDs[string(vmi.ObjectMeta.UID)] = struct{}{}
		}
	}
	for _, pod := range pods.Items {
		if podCreatedBy, ok := pod.ObjectMeta.Labels["kubevirt.io/created-by"]; ok {
			if _, ok := vmiUIDs[podCreatedBy]; ok {
				delete(pod.ObjectMeta.Labels, serviceLabelKey(lbName))
				_, err = lb.kubevirt.CoreV1().Pods(lb.namespace).Update(&pod)
				if err != nil {
					return fmt.Errorf("Failed to update pod: %s: %v", pod.ObjectMeta.Name, err)
				}
			}
		}
	}
	return nil
}

func (lb *loadbalancer) getLoadBalancerCreatePollInterval() time.Duration {
	if lb.config.CreationPollInterval > 0 {
		return time.Duration(lb.config.CreationPollInterval)
	}
	klog.Infof("Creation poll interval '%d' must be > 0. Setting to '%d'", lb.config.CreationPollInterval, defaultLoadBalancerCreatePollInterval)
	return defaultLoadBalancerCreatePollInterval
}

func buildInstanceIDMap(nodes []*corev1.Node) map[string]struct{} {
	instanceIDs := make(map[string]struct{}, len(nodes))
	for _, instanceID := range instanceIDsFromNodes(nodes) {
		instanceIDs[instanceID] = struct{}{}
	}
	return instanceIDs
}

func serviceLabelKey(lbName string) string {
	return strings.Join([]string{serviceLabelKeyPrefix, lbName}, "/")
}
