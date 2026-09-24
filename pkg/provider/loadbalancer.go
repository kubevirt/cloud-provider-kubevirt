package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	cloudprovider "k8s.io/cloud-provider"
	servicehelpers "k8s.io/cloud-provider/service/helpers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Default interval between polling the service after creation
	defaultLoadBalancerCreatePollInterval = 5 * time.Second

	// Default timeout between polling the service after creation
	defaultLoadBalancerCreatePollTimeout = 5 * time.Minute

	TenantServiceNameLabelKey      = "cluster.x-k8s.io/tenant-service-name"
	TenantServiceNamespaceLabelKey = "cluster.x-k8s.io/tenant-service-namespace"
	TenantClusterNameLabelKey      = "cluster.x-k8s.io/cluster-name"
	TenantNodeRoleLabelKey         = "cluster.x-k8s.io/role"

	// This infra-only annotation records keys owned by the mirroring provider.
	tenantAnnotationKeys = "cloud-provider.kubevirt.io/tenant-annotation-keys"
)

type loadbalancer struct {
	namespace   string
	client      client.Client
	config      LoadBalancerConfig
	infraLabels map[string]string
	recorder    record.EventRecorder
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) GetLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service) (status *corev1.LoadBalancerStatus, exists bool, err error) {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	lbService, err := lb.getLoadBalancerService(ctx, lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, false, err
	}
	if lbService == nil {
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

	lbService, err := lb.getLoadBalancerService(ctx, lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return nil, err
	}

	ports := lb.createLoadBalancerServicePorts(service)
	// LoadBalancer already exists, reconcile the mirrored fields if changed
	if lbService != nil {
		return &lbService.Status.LoadBalancer, lb.reconcileLoadBalancerService(ctx, service, lbService, ports)
	}

	vmiLabels := map[string]string{
		TenantNodeRoleLabelKey:    "worker",
		TenantClusterNameLabelKey: clusterName,
	}

	lbLabels := map[string]string{
		TenantServiceNameLabelKey:      service.Name,
		TenantServiceNamespaceLabelKey: service.Namespace,
		TenantClusterNameLabelKey:      clusterName,
	}

	for key, val := range lb.infraLabels {
		lbLabels[key] = val
	}

	lbService, err = lb.createLoadBalancerService(ctx, lbName, service, vmiLabels, lbLabels, ports)
	if err != nil {
		klog.Errorf("Failed to create LoadBalancer service: %v", err)
		return nil, err
	}

	err = wait.PollWithContext(ctx, lb.getLoadBalancerCreatePollInterval(), lb.getLoadBalancerCreatePollTimeout(), func(ctx context.Context) (bool, error) {
		if len(lbService.Status.LoadBalancer.Ingress) != 0 {
			return true, nil
		}
		var service *corev1.Service
		service, err = lb.getLoadBalancerService(ctx, lbName)
		if err != nil {
			klog.Errorf("Failed to get LoadBalancer service: %v", err)
			return false, err
		}
		if service != nil && len(service.Status.LoadBalancer.Ingress) > 0 {
			lbService = service
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		klog.Errorf("Failed to poll LoadBalancer service: %v", err)
		return nil, err
	}

	return &lbService.Status.LoadBalancer, nil
}

// UpdateLoadBalancer reconciles the managed fields in the infra Service.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (lb *loadbalancer) UpdateLoadBalancer(ctx context.Context, clusterName string, service *corev1.Service, nodes []*corev1.Node) error {
	lbName := lb.GetLoadBalancerName(ctx, clusterName, service)
	var lbService corev1.Service
	if err := lb.client.Get(ctx, client.ObjectKey{Name: lbName, Namespace: lb.namespace}, &lbService); err != nil {
		if errors.IsNotFound(err) {
			klog.Errorf("Service %s doesn't exist in namespace %s: %v", lbName, lb.namespace, err)
			return err
		}
		klog.Errorf("Failed to get Service %s in namespace %s: %v", lbName, lb.namespace, err)
		return err
	}

	ports := lb.createLoadBalancerServicePorts(service)
	// LoadBalancer already exists, reconcile the mirrored fields if changed
	return lb.reconcileLoadBalancerService(ctx, service, &lbService, ports)
}

// reconcileLoadBalancerService updates an existing infra Service so that its
// ports and the security-relevant mirrored fields (allowlisted annotations,
// loadBalancerSourceRanges, loadBalancerIP) match what the
// tenant Service is currently allowed to request. It also strips fields that
// must never be present (externalIPs), which cleans up infra Services created by
// older builds that copied tenant fields verbatim.
func (lb *loadbalancer) reconcileLoadBalancerService(ctx context.Context, service, lbService *corev1.Service, ports []corev1.ServicePort) error {
	lb.warnIgnoredFields(service)
	desiredSourceRanges, err := tenantLoadBalancerSourceRanges(service)
	if err != nil {
		return err
	}
	desiredAnnotations, err := lb.reconcileAnnotations(service, lbService.Annotations)
	if err != nil {
		return err
	}
	changed := false

	// NodePorts belong to the infra Service. Preserve API-allocated values for
	// retained ports rather than resetting them on every reconciliation.
	for i := range ports {
		for _, existing := range lbService.Spec.Ports {
			if ports[i].Name == existing.Name && ports[i].Protocol == existing.Protocol {
				ports[i].NodePort = existing.NodePort
				break
			}
		}
	}
	if !equality.Semantic.DeepEqual(ports, lbService.Spec.Ports) {
		lbService.Spec.Ports = ports
		changed = true
	}

	if !equality.Semantic.DeepEqual(desiredAnnotations, lbService.Annotations) {
		lbService.Annotations = desiredAnnotations
		changed = true
	}

	if !equality.Semantic.DeepEqual(desiredSourceRanges, lbService.Spec.LoadBalancerSourceRanges) {
		lbService.Spec.LoadBalancerSourceRanges = desiredSourceRanges
		changed = true
	}

	desiredLoadBalancerIP := ""
	if service.Spec.LoadBalancerIP != "" && lb.config.AllowTenantLoadBalancerIP != nil && *lb.config.AllowTenantLoadBalancerIP {
		desiredLoadBalancerIP = service.Spec.LoadBalancerIP
	}
	if desiredLoadBalancerIP != lbService.Spec.LoadBalancerIP {
		lbService.Spec.LoadBalancerIP = desiredLoadBalancerIP
		changed = true
	}

	// externalIPs are never propagated; drop any that a previous build copied.
	if len(lbService.Spec.ExternalIPs) > 0 {
		lbService.Spec.ExternalIPs = nil
		changed = true
	}

	if !changed {
		return nil
	}
	if err := lb.client.Update(ctx, lbService); err != nil {
		klog.Errorf("Failed to update LoadBalancer service: %v", err)
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

	lbService, err := lb.getLoadBalancerService(ctx, lbName)
	if err != nil {
		klog.Errorf("Failed to get LoadBalancer service: %v", err)
		return err
	}
	if lbService != nil {
		if err = lb.client.Delete(ctx, lbService); err != nil {
			klog.Errorf("Failed to delete LoadBalancer service: %v", err)
			return err
		}
	}

	return nil
}

func (lb *loadbalancer) getLoadBalancerService(ctx context.Context, lbName string) (*corev1.Service, error) {
	var service corev1.Service
	if err := lb.client.Get(ctx, client.ObjectKey{Name: lbName, Namespace: lb.namespace}, &service); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &service, nil
}

func (lb *loadbalancer) createLoadBalancerService(ctx context.Context, lbName string, service *corev1.Service, vmiLabels map[string]string, lbLabels map[string]string, ports []corev1.ServicePort) (*corev1.Service, error) {
	lb.warnIgnoredFields(service)
	sourceRanges, err := tenantLoadBalancerSourceRanges(service)
	if err != nil {
		return nil, err
	}
	annotations, err := lb.reconcileAnnotations(service, nil)
	if err != nil {
		return nil, err
	}
	lbService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        lbName,
			Namespace:   lb.namespace,
			Annotations: annotations,
			Labels:      lbLabels,
		},
		Spec: corev1.ServiceSpec{
			Ports:                    ports,
			Type:                     corev1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy:    service.Spec.ExternalTrafficPolicy,
			LoadBalancerSourceRanges: sourceRanges,
		},
	}
	// Give controller privilege above selectorless
	if lb.config.EnableEPSController != nil && *lb.config.EnableEPSController && service.Spec.ExternalTrafficPolicy == corev1.ServiceExternalTrafficPolicyTypeLocal {
		lbService.Spec.Selector = nil
	} else if lb.config.Selectorless != nil && *lb.config.Selectorless {
		lbService.Spec.Selector = nil
	} else {
		lbService.Spec.Selector = vmiLabels
	}

	if lb.config.AllowTenantLoadBalancerIP != nil && *lb.config.AllowTenantLoadBalancerIP {
		lbService.Spec.LoadBalancerIP = service.Spec.LoadBalancerIP
	}

	if err := lb.client.Create(ctx, lbService); err != nil {
		klog.Errorf("Failed to create LB %s: %v", lbName, err)
		return nil, err
	}
	return lbService, nil
}

// allowedAnnotations returns the subset of the tenant Service's annotations that
// the infra operator has explicitly allowlisted via LoadBalancerConfig.
// Ownership metadata, kubectl's saved configuration and the legacy source-range
// annotation are excluded even if allowlisted. Source ranges are handled as a
// validated field, not passed through as an annotation.
func (lb *loadbalancer) allowedAnnotations(service *corev1.Service) map[string]string {
	if len(lb.config.AllowedAnnotations) == 0 || len(service.Annotations) == 0 {
		return nil
	}
	var annotations map[string]string
	for _, key := range lb.config.AllowedAnnotations {
		if key == tenantAnnotationKeys || key == corev1.LastAppliedConfigAnnotation || key == corev1.AnnotationLoadBalancerSourceRangesKey {
			continue
		}
		val, ok := service.Annotations[key]
		if !ok {
			continue
		}
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[key] = val
	}
	return annotations
}

// reconcileAnnotations preserves infra-owned annotations and replaces only keys
// previously managed by this provider. Legacy Services have no ownership record:
// only keys still matching the current tenant value can be identified for cleanup.
func (lb *loadbalancer) reconcileAnnotations(service *corev1.Service, existing map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(existing)+1)
	for key, value := range existing {
		result[key] = value
	}
	if raw, tracked := existing[tenantAnnotationKeys]; tracked {
		var keys []string
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return nil, fmt.Errorf("invalid infra annotation %s: %w", tenantAnnotationKeys, err)
		}
		for _, key := range keys {
			delete(result, key)
		}
	} else {
		for key, value := range service.Annotations {
			if current, present := result[key]; present && current == value {
				delete(result, key)
			}
		}
	}
	keys := []string{}
	for key, value := range lb.allowedAnnotations(service) {
		result[key] = value
		keys = append(keys, key)
	}
	sort.Strings(keys)
	encoded, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	result[tenantAnnotationKeys] = string(encoded)
	return result, nil
}

func (lb *loadbalancer) warnIgnoredFields(service *corev1.Service) {
	if lb.recorder == nil {
		return
	}
	if len(service.Spec.ExternalIPs) > 0 {
		lb.recorder.Event(service, corev1.EventTypeWarning, "ExternalIPsIgnored", "spec.externalIPs is not propagated to the infrastructure Service")
	}
	if service.Spec.LoadBalancerIP != "" && (lb.config.AllowTenantLoadBalancerIP == nil || !*lb.config.AllowTenantLoadBalancerIP) {
		lb.recorder.Event(service, corev1.EventTypeWarning, "LoadBalancerIPIgnored", "spec.loadBalancerIP is not propagated: tenant-requested infrastructure addresses are disabled by the infrastructure operator")
	}
}

// tenantLoadBalancerSourceRanges returns the tenant Service's requested source
// ranges, reading spec.loadBalancerSourceRanges and falling back to the legacy
// annotation. Returns nil when none are requested.
func tenantLoadBalancerSourceRanges(service *corev1.Service) ([]string, error) {
	// Preserve absence instead of introducing the helper's IPv4 allow-all default.
	if len(service.Spec.LoadBalancerSourceRanges) == 0 && strings.TrimSpace(service.Annotations[corev1.AnnotationLoadBalancerSourceRangesKey]) == "" {
		return nil, nil
	}
	ranges, err := servicehelpers.GetLoadBalancerSourceRanges(service)
	if err != nil {
		return nil, err
	}
	result := ranges.StringSlice()
	sort.Strings(result)
	return result, nil
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

func (lb *loadbalancer) getLoadBalancerCreatePollInterval() time.Duration {
	return convertLoadBalancerCreatePollConfig(lb.config.CreationPollInterval, defaultLoadBalancerCreatePollInterval, "interval")
}

func (lb *loadbalancer) getLoadBalancerCreatePollTimeout() time.Duration {
	return convertLoadBalancerCreatePollConfig(lb.config.CreationPollTimeout, defaultLoadBalancerCreatePollTimeout, "timeout")
}

func convertLoadBalancerCreatePollConfig(configValue *int, defaultValue time.Duration, name string) time.Duration {
	if configValue == nil {
		klog.Infof("Setting creation poll %s to default value '%d'", name, defaultValue)
		return defaultValue
	}
	if *configValue <= 0 {
		klog.Infof("Creation poll %s %d' must be > 0. Setting to '%d'", name, *configValue, defaultValue)
		return defaultValue
	}
	return time.Duration(*configValue) * time.Second

}
