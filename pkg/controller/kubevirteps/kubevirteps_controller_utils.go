package kubevirteps

import (
	"sync"

	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	utilnet "k8s.io/utils/net"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

// source: https://github.com/kubernetes/endpointslice/blob/master/utils.go#L280
func getAddressTypesForService(service *v1.Service) sets.Set[discovery.AddressType] {
	serviceSupportedAddresses := sets.New[discovery.AddressType]()

	// If
	for _, family := range service.Spec.IPFamilies {
		if family == v1.IPv4Protocol {
			serviceSupportedAddresses.Insert(discovery.AddressTypeIPv4)
		}

		if family == v1.IPv6Protocol {
			serviceSupportedAddresses.Insert(discovery.AddressTypeIPv6)
		}
	}

	if serviceSupportedAddresses.Len() > 0 {
		return serviceSupportedAddresses // we have found families for this service
	}

	// If no families are found, we will use the ClusterIP to determine the address type
	if len(service.Spec.ClusterIP) > 0 && service.Spec.ClusterIP != v1.ClusterIPNone { // headfull
		addrType := discovery.AddressTypeIPv4
		if utilnet.IsIPv6String(service.Spec.ClusterIP) {
			addrType = discovery.AddressTypeIPv6
		}
		serviceSupportedAddresses.Insert(addrType)
		klog.V(2).Info("Couldn't find ipfamilies for service. This could happen if controller manager is connected to an old apiserver that does not support ip families yet. EndpointSlices for this Service will use addressType as the IP Family based on familyOf(ClusterIP).", "service", klog.KObj(service), "addressType", addrType, "clusterIP", service.Spec.ClusterIP)
		return serviceSupportedAddresses
	}

	serviceSupportedAddresses.Insert(discovery.AddressTypeIPv4)
	serviceSupportedAddresses.Insert(discovery.AddressTypeIPv6)
	klog.V(2).Info("Couldn't find ipfamilies for headless service, likely because controller manager is likely connected to an old apiserver that does not support ip families yet. The service endpoint slice will use dual stack families until api-server default it correctly", "service", klog.KObj(service))
	return serviceSupportedAddresses
}

// The tenantESPTracker is used to keep track of which EndpointSlices are being watched by the KubevirtCloudController.
// This is necessary because the KubevirtCloudController needs to watch EndpointSlices in the tenant cluster that correspond
// to Services in the infra cluster. The KubevirtCloudController needs to know which EndpointSlices to watch so that it can
// update the corresponding EndpointSlices in the infra cluster when the tenant cluster's EndpointSlices change.
type tenantEPSTracker struct {
	sync.RWMutex
	register []types.NamespacedName
}

func (t *tenantEPSTracker) add(eps *discovery.EndpointSlice) {
	t.Lock()
	defer t.Unlock()
	klog.Infof("Adding EndpointSlice %s to the tenantEPSTracker", eps.Name)
	name := types.NamespacedName{
		Namespace: eps.Namespace,
		Name:      eps.Name,
	}
	t.register = append(t.register, name)
}

func (t *tenantEPSTracker) remove(eps *discovery.EndpointSlice) {
	t.Lock()
	defer t.Unlock()
	klog.Infof("Remove EndpointSlice %s to the tenantEPSTracker", eps.Name)
	name := types.NamespacedName{
		Namespace: eps.Namespace,
		Name:      eps.Name,
	}
	for i, n := range t.register {
		if n == name {
			t.register = append(t.register[:i], t.register[i+1:]...)
			return
		}
	}
}

func (t *tenantEPSTracker) contains(eps *discovery.EndpointSlice) bool {
	t.RLock()
	defer t.RUnlock()
	name := types.NamespacedName{
		Namespace: eps.Namespace,
		Name:      eps.Name,
	}
	for _, n := range t.register {
		if n == name {
			return true
		}
	}
	return false
}

// tenantNodeConditions folds the conditions of every tenant endpoint on one node (one VM).
type tenantNodeConditions struct {
	ready              bool // at least one endpoint is ready
	serving            bool // at least one endpoint is serving
	terminating        bool // every endpoint is terminating
	servingTerminating bool // at least one endpoint is serving and terminating
}

func newTenantNodeConditions() *tenantNodeConditions {
	return &tenantNodeConditions{terminating: true}
}

// add folds one tenant endpoint's conditions in. Nil conditions are read as the EndpointSlice
// API defines them (ready and serving true, terminating false). A terminating endpoint is
// never counted as ready, even when ready is left unset.
func (n *tenantNodeConditions) add(c discovery.EndpointConditions) {
	terminating := c.Terminating != nil && *c.Terminating
	ready := (c.Ready == nil || *c.Ready) && !terminating
	serving := c.Serving == nil || *c.Serving
	n.ready = n.ready || ready
	n.serving = n.serving || serving
	n.terminating = n.terminating && terminating
	n.servingTerminating = n.servingTerminating || (serving && terminating)
}

// forVMI returns the infra endpoint's conditions: the tenant endpoints' conditions, and only
// while the VM is running. A stopped VM (Failed or Succeeded) is terminating whatever the
// tenant still says.
//
// The VM is terminating when every tenant endpoint on it is, and also when none is ready but
// one is serving and terminating: kube-proxy and Cilium fall back to serving and terminating
// endpoints when no endpoint is ready, so a pod that is still starting must not hide a pod
// that is draining on the same VM.
func (n *tenantNodeConditions) forVMI(phase kubevirtv1.VirtualMachineInstancePhase) (ready, serving, terminating bool) {
	running := phase == kubevirtv1.Running
	stopped := phase == kubevirtv1.Failed || phase == kubevirtv1.Succeeded
	terminating = n.terminating || (!n.ready && n.servingTerminating)
	return running && n.ready, running && n.serving, stopped || terminating
}
