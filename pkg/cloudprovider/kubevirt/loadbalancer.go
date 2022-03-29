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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/client-go/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Prefix of the service label to put on VMIs and pods
	serviceLabelKeyPrefix = "cloud.kubevirt.io"
	// Default interval in seconds between polling the service after creation
	defaultLoadBalancerCreatePollInterval = 5
)

type loadbalancer struct {
	namespace string
	// cloudProviderClient is the client for the underlying KubeVirt cluster where VMs are created.
	cloudProviderClient client.Client
	// client is the kuberentes cluster client that is constructed out of the created VMs in the KubeVirt cluster.
	client *kubernetes.Clientset
	config LoadBalancerConfig
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	lbService, serviceExists, err := lb.getLoadBalancerService(ctx, lbName)
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
	if service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		matchedNodes, err := lb.filterNodes(service, nodes)
		if err != nil {
			klog.Errorf("Failed to filter nodes: %v", err)
			return nil, err
		}

		nodes = matchedNodes
	}

	err := lb.applyServiceLabels(ctx, lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return nil, err
	}
	err = lb.ensureServiceLabelsDeleted(ctx, lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return nil, err
	}

	lbService, lbExists, err := lb.getLoadBalancerService(ctx, lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, err
	}
	if lbExists {
		if !equality.Semantic.DeepEqual(service.Spec.Ports, lbService.Spec.Ports) {
			lbService.Spec.Ports = lb.createLoadBalancerServicePorts(service)
			if err := lb.cloudProviderClient.Update(ctx, lbService); err != nil {
				klog.Errorf("Failed to update LoadBalancer service: %v", err)
				return nil, err
			}
		}
		return &lbService.Status.LoadBalancer, nil
	}

	lbService, err = lb.createLoadBalancerService(ctx, lbName, service)
	if err != nil {
		klog.Errorf("Failed to create LoadBalancer service: %v", err)
		return nil, err
	}

	err = wait.PollUntil(lb.getLoadBalancerCreatePollInterval()*time.Second, func() (bool, error) {
		if len(lbService.Status.LoadBalancer.Ingress) != 0 {
			return true, nil
		}
		service, exists, err := lb.getLoadBalancerService(ctx, lbName)
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
	if service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		matchedNodes, err := lb.filterNodes(service, nodes)
		if err != nil {
			klog.Errorf("Failed to filter nodes: %v", err)
			return err
		}

		nodes = matchedNodes
	}

	err := lb.applyServiceLabels(ctx, lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		klog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return err
	}
	err = lb.ensureServiceLabelsDeleted(ctx, lbName, service.ObjectMeta.Name, nodes)
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

	lbService, lbExists, err := lb.getLoadBalancerService(ctx, lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return err
	}
	if lbExists {
		if err := lb.cloudProviderClient.Delete(ctx, lbService); err != nil {
			klog.Errorf("Failed to delete LoadBalancer service: %v", err)
			return err
		}
	}

	err = lb.ensureServiceLabelsDeleted(ctx, lbName, service.ObjectMeta.Name, []*corev1.Node{})
	if err != nil {
		klog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return err
	}

	return nil
}

func (lb *loadbalancer) getLoadBalancerService(ctx context.Context, lbName string) (*corev1.Service, bool, error) {
	var service corev1.Service
	if err := lb.cloudProviderClient.Get(ctx, client.ObjectKey{Name: lbName, Namespace: lb.namespace}, &service); err != nil {
		if errors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &service, true, nil
}

func (lb *loadbalancer) createLoadBalancerService(ctx context.Context, lbName string, service *corev1.Service) (*corev1.Service, error) {
	ports := lb.createLoadBalancerServicePorts(service)
	lbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbName,
			Namespace: lb.namespace,
			Annotations: service.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: map[string]string{serviceLabelKey(lbName): service.ObjectMeta.Name},
			Type:     corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: service.Spec.ExternalTrafficPolicy,
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

	if err := lb.cloudProviderClient.Create(ctx, lbService); err != nil {
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

func (lb *loadbalancer) applyServiceLabels(ctx context.Context, lbName, serviceName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	var allVmis kubevirtv1.VirtualMachineInstanceList
	if err := lb.cloudProviderClient.List(ctx, &allVmis, client.InNamespace(lb.namespace)); err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	var vmiUIDs []string

	// Apply labels to all VMIs for the service to match
	for _, vmi := range allVmis.Items {
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; ok {
			vmi.ObjectMeta.Labels[serviceLabelKey(lbName)] = serviceName
			if err := lb.cloudProviderClient.Update(ctx, &vmi); err != nil {
				klog.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			} else {
				// Remember updated VMI UIDs to find the corresponding pods
				vmiUIDs = append(vmiUIDs, string(vmi.ObjectMeta.UID))
			}
		}
	}

	// Find all pods created by a VMI
	var vmiPods corev1.PodList
	createdByVMIReq, err := labels.NewRequirement("kubevirt.io/created-by", selection.In, vmiUIDs)
	if err != nil {
		return fmt.Errorf("Failed to create Pod label selector: %v", err)
	}
	if err := lb.cloudProviderClient.List(ctx, &vmiPods, client.InNamespace(lb.namespace), client.MatchingLabelsSelector{
		Selector: labels.NewSelector().Add(*createdByVMIReq),
	}); err != nil {
		return fmt.Errorf("Failed to list VMI pods: %v", err)
	}

	// Apply labels to all found pods
	for _, pod := range vmiPods.Items {
		pod.ObjectMeta.Labels[serviceLabelKey(lbName)] = serviceName
		if err := lb.cloudProviderClient.Update(ctx, &pod); err != nil {
			klog.Errorf("Failed to update pod %s: %v", pod.ObjectMeta.Name, err)
		}
	}
	return nil
}

func (lb *loadbalancer) ensureServiceLabelsDeleted(ctx context.Context, lbName, svcName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	listOptions := &client.ListOptions{
		Namespace: lb.namespace,
		LabelSelector: labels.SelectorFromSet(labels.Set{
			serviceLabelKey(lbName): svcName,
		}),
	}
	var vmis kubevirtv1.VirtualMachineInstanceList
	if err := lb.cloudProviderClient.List(ctx, &vmis, listOptions); err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	var pods corev1.PodList
	if err := lb.cloudProviderClient.List(ctx, &pods, listOptions); err != nil {
		return fmt.Errorf("Failed to list pods: %v", err)
	}
	vmiUIDs := make(map[string]struct{}, len(instanceIDs))

	// Delete labels on those VMIs & pods which do not have a corresponding node object
	for _, vmi := range vmis.Items {
		if _, ok := instanceIDs[vmi.ObjectMeta.Name]; !ok {
			delete(vmi.ObjectMeta.Labels, serviceLabelKey(lbName))
			if err := lb.cloudProviderClient.Update(ctx, &vmi); err != nil {
				return fmt.Errorf("Failed to update VMI %s: %v", vmi.ObjectMeta.Name, err)
			}
			vmiUIDs[string(vmi.ObjectMeta.UID)] = struct{}{}
		}
	}
	for _, pod := range pods.Items {
		if podCreatedBy, ok := pod.ObjectMeta.Labels["kubevirt.io/created-by"]; ok {
			if _, ok := vmiUIDs[podCreatedBy]; ok {
				delete(pod.ObjectMeta.Labels, serviceLabelKey(lbName))
				if err := lb.cloudProviderClient.Update(ctx, &pod); err != nil {
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

func (lb *loadbalancer) filterNodes(service *corev1.Service, nodes []*corev1.Node) ([]*corev1.Node, error) {
	labelSelector := metav1.LabelSelector{MatchLabels: service.Spec.Selector}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		Limit:         100,
	}

	podsList, err := lb.client.CoreV1().Pods(service.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	var nodesNames = map[string]struct{}{}
	for _, pod := range podsList.Items {
		for key, val := range pod.Labels {
			if s, ok := service.Spec.Selector[key]; ok {
				if val == s {
					nodesNames[pod.Spec.NodeName] = struct{}{}
				}
			}
		}
	}

	var matchedNodes []*corev1.Node
	for _, node := range nodes {
		if _, ok := nodesNames[node.Name]; ok {
			matchedNodes = append(matchedNodes, node)
		}
	}

	return matchedNodes, nil
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
