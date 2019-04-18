package kubevirt

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/cloudprovider"
	v1 "kubevirt.io/kubevirt/pkg/api/v1"
	"kubevirt.io/kubevirt/pkg/kubecli"
)

const (
	// Prefix of the service label to put on VMIs and pods
	serviceLabelKeyPrefix = "cloud.kubevirt.io"
	// Interval in seconds between polling the service after creation
	loadBalancerCreatePollIntervalSeconds = 5
)

type loadbalancer struct {
	namespace string
	kubevirt  kubecli.KubevirtClient
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	lbService, serviceExists, err := lb.getLoadBalancerService(lbName)
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
		glog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return nil, err
	}
	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		glog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
		return nil, err
	}

	lbService, lbExists, err := lb.getLoadBalancerService(lbName)
	if err != nil {
		glog.Errorf("Failed to get LoadBalancer service: %v", err)
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
		glog.Errorf("Failed to create LoadBalancer service: %v", err)
		return nil, err
	}

	err = wait.PollUntil(loadBalancerCreatePollIntervalSeconds*time.Second, func() (bool, error) {
		if len(lbService.Status.LoadBalancer.Ingress) != 0 {
			return true, nil
		}
		service, exists, err := lb.getLoadBalancerService(lbName)
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
func (lb *loadbalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	err := lb.applyServiceLabels(lbName, service.ObjectMeta.Name, nodes)
	if err != nil {
		glog.Errorf("Failed to add nodes to LoadBalancer service: %v", err)
		return err
	}
	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, nodes)
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
func (lb *loadbalancer) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *corev1.Service) error {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)

	lbService, lbExists, err := lb.getLoadBalancerService(lbName)
	if err != nil {
		glog.Errorf("Failed to get LoadBalancer service: %v", err)
		return err
	}
	if lbExists {
		err = lb.kubevirt.CoreV1().Services(lb.namespace).Delete(lbService.ObjectMeta.Name, &metav1.DeleteOptions{})
		if err != nil {
			glog.Errorf("Failed to delete LoadBalancer service: %v", err)
			return err
		}
	}

	err = lb.ensureServiceLabelsDeleted(lbName, service.ObjectMeta.Name, []*corev1.Node{})
	if err != nil {
		glog.Errorf("Failed to delete nodes from LoadBalancer service: %v", err)
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
		glog.Errorf("Failed to create LB %s: %v", lbName, err)
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
	serviceLabelKey := serviceLabelKey(lbName)
	allVmis, err := lb.kubevirt.VirtualMachineInstance(lb.namespace).List(&metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Failed to list VMIs: %v", err)
	}
	vmiUIDs := make([]string, len(instanceIDs))

	// Apply labels to all VMIs for the service to match
	for _, oldVMI := range allVmis.Items {
		if _, ok := instanceIDs[oldVMI.Name]; ok {
			newVMI := oldVMI.DeepCopy()
			addLabel(newVMI.Labels, serviceLabelKey, serviceName)

			err := lb.patchVMI(&oldVMI, newVMI)
			if err != nil {
				glog.Errorf("Failed to update VMI %s: %v", oldVMI.Name, err)
			} else {
				// Remember updated VMI UIDs to find the correspomding pods
				vmiUIDs = append(vmiUIDs, string(oldVMI.UID))
			}
		}
	}

	// Find all pods created by a VMI
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubevirt.io/created-by in (%s)", strings.Join(vmiUIDs, ", ")),
	}
	vmiPods, err := lb.kubevirt.CoreV1().Pods(lb.namespace).List(listOptions)

	// Apply labels to all found pods
	for _, oldPod := range vmiPods.Items {
		newPod := oldPod.DeepCopy()
		addLabel(newPod.Labels, serviceLabelKey, serviceName)

		err := lb.patchPod(&oldPod, newPod)
		if err != nil {
			glog.Errorf("Failed to update pod %s: %v", oldPod.Name, err)
		}
	}
	return nil
}

func (lb *loadbalancer) ensureServiceLabelsDeleted(lbName, svcName string, nodes []*corev1.Node) error {
	instanceIDs := buildInstanceIDMap(nodes)
	serviceLabelKey := serviceLabelKey(lbName)
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", serviceLabelKey, svcName),
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
	for _, oldVMI := range vmis.Items {
		if _, ok := instanceIDs[oldVMI.Name]; !ok {
			newVMI := oldVMI.DeepCopy()
			delete(newVMI.Labels, serviceLabelKey)
			err = lb.patchVMI(&oldVMI, newVMI)
			if err != nil {
				return fmt.Errorf("Failed to update VMI %s: %v", oldVMI.Name, err)
			}
			vmiUIDs[string(oldVMI.UID)] = struct{}{}
		}
	}
	for _, oldPod := range pods.Items {
		if podCreatedBy, ok := oldPod.Labels["kubevirt.io/created-by"]; ok {
			if _, ok := vmiUIDs[podCreatedBy]; ok {
				newPod := oldPod.DeepCopy()
				delete(newPod.Labels, serviceLabelKey)
				err = lb.patchPod(&oldPod, newPod)
				if err != nil {
					return fmt.Errorf("Failed to update pod: %s: %v", oldPod.Name, err)
				}
			}
		}
	}
	return nil
}

func (lb *loadbalancer) patchVMI(old *v1.VirtualMachineInstance, new *v1.VirtualMachineInstance) error {
	patch, err := createMergePatch(old, new)
	if err != nil {
		return err
	}
	_, err = lb.kubevirt.VirtualMachineInstance(lb.namespace).Patch(new.Name, types.MergePatchType, patch)
	if err != nil {
		return err
	}
	return nil
}

func (lb *loadbalancer) patchPod(old *corev1.Pod, new *corev1.Pod) error {
	patch, err := createMergePatch(old, new)
	if err != nil {
		return err
	}
	_, err = lb.kubevirt.CoreV1().Pods(lb.namespace).Patch(new.Name, types.MergePatchType, patch)
	if err != nil {
		return err
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

func serviceLabelKey(lbName string) string {
	return strings.Join([]string{serviceLabelKeyPrefix, lbName}, "/")
}

func addLabel(labels map[string]string, key, value string) {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[key] = value
}

func createMergePatch(obj1 metav1.Object, obj2 metav1.Object) ([]byte, error) {
	t1, t2 := reflect.TypeOf(obj1), reflect.TypeOf(obj2)
	if t1 != t2 {
		return nil, fmt.Errorf("cannot patch two objects of different type: %q - %q", t1, t2)
	}
	if t1.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("type has to be of kind pointer but got %q", t1)
	}

	obj1Data, err := json.Marshal(obj1)
	if err != nil {
		return nil, err
	}

	obj2Data, err := json.Marshal(obj2)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.CreateMergePatch(obj1Data, obj2Data)
	if err != nil {
		glog.Errorf("*** %v", err)
		return nil, err
	}
	glog.Infof("*********************** %s\n%s\n%s", patch, obj1Data, obj2Data)
	return patch, err
}
