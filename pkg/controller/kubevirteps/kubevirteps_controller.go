package kubevirteps

import (
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	controllersmetrics "k8s.io/component-base/metrics/prometheus/controllers"
	endpointsliceutil "k8s.io/endpointslice/util"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/cloud-provider-kubevirt/pkg/provider"
	"strings"
	"time"
)

const (
	ControllerName = controllerName("kubevirt_eps_controller")
)

type controllerName string

func (c controllerName) dashed() string {
	// replace underscores with dashes
	return strings.ReplaceAll(string(c), "_", "-")
}

func (c controllerName) String() string {
	return string(c)
}

type Controller struct {
	tenantClient     kubernetes.Interface
	tenantFactory    informers.SharedInformerFactory
	tenantEPSTracker tenantEPSTracker

	infraClient  kubernetes.Interface
	infraDynamic dynamic.Interface
	infraFactory informers.SharedInformerFactory

	infraNamespace string
	queue          workqueue.RateLimitingInterface
	maxRetries     int

	maxEndPointsPerSlice int
}

func NewKubevirtEPSController(
	tenantClient kubernetes.Interface,
	infraClient kubernetes.Interface,
	infraDynamic dynamic.Interface,
	infraNamespace string) *Controller {

	tenantFactory := informers.NewSharedInformerFactory(tenantClient, 0)
	infraFactory := informers.NewSharedInformerFactoryWithOptions(infraClient, 0, informers.WithNamespace(infraNamespace))
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	return &Controller{
		tenantClient:         tenantClient,
		tenantFactory:        tenantFactory,
		tenantEPSTracker:     tenantEPSTracker{},
		infraClient:          infraClient,
		infraDynamic:         infraDynamic,
		infraFactory:         infraFactory,
		infraNamespace:       infraNamespace,
		queue:                queue,
		maxRetries:           25,
		maxEndPointsPerSlice: 100,
	}
}

type ReqType string

const (
	AddReq    ReqType = "add"
	UpdateReq ReqType = "update"
	DeleteReq ReqType = "delete"
)

type Request struct {
	ReqType ReqType
	Obj     interface{}
	OldObj  interface{}
}

func newRequest(reqType ReqType, obj interface{}, oldObj interface{}) *Request {
	return &Request{
		ReqType: reqType,
		Obj:     obj,
		OldObj:  oldObj,
	}
}

func (c *Controller) Init() error {

	// Act on events from Services on the infra cluster. These are created by the EnsureLoadBalancer function.
	// We need to watch for these events so that we can update the EndpointSlices in the infra cluster accordingly.
	_, err := c.infraFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// cast obj to Service
			svc := obj.(*v1.Service)
			// Only act on Services of type LoadBalancer
			if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service added: %v/%v", svc.Namespace, svc.Name)
				c.queue.Add(newRequest(AddReq, obj, nil))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			// cast obj to Service
			newSvc := newObj.(*v1.Service)
			// Only act on Services of type LoadBalancer
			if newSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service updated: %v/%v", newSvc.Namespace, newSvc.Name)
				c.queue.Add(newRequest(UpdateReq, newObj, oldObj))
			}
		},
		DeleteFunc: func(obj interface{}) {
			// cast obj to Service
			svc := obj.(*v1.Service)
			// Only act on Services of type LoadBalancer
			if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service deleted: %v/%v", svc.Namespace, svc.Name)
				c.queue.Add(newRequest(DeleteReq, obj, nil))
			}
		},
	})
	if err != nil {
		return err
	}

	// Monitor endpoint slices that we are interested in based on known services in the infra cluster
	_, err = c.tenantFactory.Discovery().V1().EndpointSlices().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			eps := obj.(*discovery.EndpointSlice)
			if c.tenantEPSTracker.contains(eps) {
				klog.Infof("get Infra Service for Tenant EndpointSlice: %v/%v", eps.Namespace, eps.Name)
				infraSvc, err := c.getInfraServiceFromTenantEPS(context.TODO(), eps)
				if err != nil {
					klog.Errorf("Failed to get Service in Infra cluster for EndpointSlice %s/%s: %v", eps.Namespace, eps.Name, err)
					return
				}
				klog.Infof("EndpointSlice added: %v/%v", eps.Namespace, eps.Name)
				c.queue.Add(newRequest(AddReq, infraSvc, nil))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			eps := newObj.(*discovery.EndpointSlice)
			if c.tenantEPSTracker.contains(eps) {
				klog.Infof("get Infra Service for Tenant EndpointSlice: %v/%v", eps.Namespace, eps.Name)
				infraSvc, err := c.getInfraServiceFromTenantEPS(context.TODO(), eps)
				if err != nil {
					klog.Errorf("Failed to get Service in Infra cluster for EndpointSlice %s/%s: %v", eps.Namespace, eps.Name, err)
					return
				}
				klog.Infof("EndpointSlice updated: %v/%v", eps.Namespace, eps.Name)
				c.queue.Add(newRequest(UpdateReq, infraSvc, nil))
			}
		},
		DeleteFunc: func(obj interface{}) {
			eps := obj.(*discovery.EndpointSlice)
			if c.tenantEPSTracker.contains(eps) {
				c.tenantEPSTracker.remove(eps)
				klog.Infof("get Infra Service for Tenant EndpointSlice: %v/%v", eps.Namespace, eps.Name)
				infraSvc, err := c.getInfraServiceFromTenantEPS(context.TODO(), eps)
				if err != nil {
					klog.Errorf("Failed to get Service in Infra cluster for EndpointSlice %s/%s: %v", eps.Namespace, eps.Name, err)
					return
				}
				klog.Infof("EndpointSlice deleted: %v/%v", eps.Namespace, eps.Name)
				c.queue.Add(newRequest(DeleteReq, infraSvc, nil))
			}
		},
	})
	if err != nil {
		return err
	}

	//TODO: Add informer for EndpointSlices in the infra cluster to watch for (unwanted) changes
	return nil
}

// Run starts an asynchronous loop that monitors and updates GKENetworkParamSet in the cluster.
func (c *Controller) Run(numWorkers int, stopCh <-chan struct{}, controllerManagerMetrics *controllersmetrics.ControllerManagerMetrics) {
	defer utilruntime.HandleCrash()

	ctx, cancelFn := context.WithCancel(context.Background())
	defer cancelFn()
	defer c.queue.ShutDown()

	klog.Infof(fmt.Sprintf("Starting %s", ControllerName))
	defer klog.Infof(fmt.Sprintf("Shutting down %s", ControllerName))
	controllerManagerMetrics.ControllerStarted(ControllerName.String())
	defer controllerManagerMetrics.ControllerStopped(ControllerName.String())

	c.tenantFactory.Start(stopCh)
	c.infraFactory.Start(stopCh)

	if !cache.WaitForNamedCacheSync(ControllerName.String(), stopCh,
		c.infraFactory.Core().V1().Services().Informer().HasSynced,
		c.tenantFactory.Discovery().V1().EndpointSlices().Informer().HasSynced) {
		return
	}

	for i := 0; i < numWorkers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-stopCh
}

// worker pattern adapted from https://github.com/kubernetes/client-go/blob/master/examples/workqueue/main.go
func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextItem(ctx) {
	}
}

func (c *Controller) processNextItem(ctx context.Context) bool {
	req, quit := c.queue.Get()
	if quit {
		return false
	}

	defer c.queue.Done(req)

	err := c.reconcile(ctx, req.(*Request))

	if err == nil {
		c.queue.Forget(req)
	} else if c.queue.NumRequeues(req) < c.maxRetries {
		c.queue.AddRateLimited(req)
	} else {
		c.queue.Forget(req)
		klog.Errorf("Dropping object out of queue after too many retries: %v", req)
		utilruntime.HandleError(err)
	}

	return true
}

// getInfraServiceFromTenantEPS returns the Service in the infra cluster that is associated with the given tenant endpoint slice.
func (c *Controller) getInfraServiceFromTenantEPS(ctx context.Context, slice *discovery.EndpointSlice) (*v1.Service, error) {
	infraServices, err := c.infraClient.CoreV1().Services(c.infraNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s,%s=%s", kubevirt.TenantServiceNameLabelKey, slice.Labels["kubernetes.io/service-name"],
			kubevirt.TenantServiceNamespaceLabelKey, slice.Namespace)})
	if err != nil {
		klog.Errorf("Failed to get Service in Infra for EndpointSlice %s in namespace %s: %v", slice.Name, slice.Namespace, err)
		return nil, err
	}
	if len(infraServices.Items) > 1 {
		// This should never be possible, only one service should exist for a given tenant endpoint slice
		klog.Errorf("Multiple services found for tenant endpoint slice %s in namespace %s", slice.Name, slice.Namespace)
		return nil, errors.New("multiple services found for tenant endpoint slice")
	}
	if len(infraServices.Items) == 1 {
		return &infraServices.Items[0], nil
	}
	// No service found, possible if service is deleted.
	return nil, nil
}

// getTenantEPSFromInfraService returns the EndpointSlices in the tenant cluster that are associated with the given infra service.
func (c *Controller) getTenantEPSFromInfraService(ctx context.Context, svc *v1.Service) ([]*discovery.EndpointSlice, error) {
	var tenantEPSSlices []*discovery.EndpointSlice
	tenantServiceName := svc.Labels[kubevirt.TenantServiceNameLabelKey]
	tenantServiceNamespace := svc.Labels[kubevirt.TenantServiceNamespaceLabelKey]
	clusterName := svc.Labels[kubevirt.TenantClusterNameLabelKey]
	klog.Infof("Searching for endpoints on tenant cluster %s for service %s in namespace %s.", clusterName, tenantServiceName, tenantServiceNamespace)
	result, err := c.tenantClient.DiscoveryV1().EndpointSlices(tenantServiceNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", discovery.LabelServiceName, tenantServiceName)})
	if err != nil {
		klog.Errorf("Failed to get EndpointSlices for Service %s in namespace %s: %v", tenantServiceName,
			tenantServiceNamespace, err)
		return nil, err
	}
	for _, eps := range result.Items {
		c.tenantEPSTracker.add(&eps)
		tenantEPSSlices = append(tenantEPSSlices, &eps)
	}
	return tenantEPSSlices, nil
}

// getInfraEPSFromInfraService returns the EndpointSlices in the infra cluster that are associated with the given infra service.
func (c *Controller) getInfraEPSFromInfraService(ctx context.Context, svc *v1.Service) ([]*discovery.EndpointSlice, error) {
	var infraEPSSlices []*discovery.EndpointSlice
	klog.Infof("Searching for endpoints on infra cluster for service %s in namespace %s.", svc.Name, svc.Namespace)
	result, err := c.infraClient.DiscoveryV1().EndpointSlices(svc.Namespace).List(ctx,
		metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", discovery.LabelServiceName, svc.Name)})
	if err != nil {
		klog.Errorf("Failed to get EndpointSlices for Service %s in namespace %s: %v", svc.Name, svc.Namespace, err)
		return nil, err
	}
	for _, eps := range result.Items {
		infraEPSSlices = append(infraEPSSlices, &eps)
	}
	return infraEPSSlices, nil
}

func (c *Controller) reconcile(ctx context.Context, r *Request) error {
	service, ok := r.Obj.(*v1.Service)
	if !ok {
		return errors.New("could not cast object to service")
	}

	if service.Labels[kubevirt.TenantServiceNameLabelKey] == "" ||
		service.Labels[kubevirt.TenantServiceNamespaceLabelKey] == "" ||
		service.Labels[kubevirt.TenantClusterNameLabelKey] == "" {
		klog.Infof("This LoadBalancer Service: %s is not managed by the %s. Skipping.", service.Name, ControllerName)
		return nil
	}
	klog.Infof("Reconciling: %v", service.Name)

	serviceDeleted := false
	svc, err := c.infraFactory.Core().V1().Services().Lister().Services(c.infraNamespace).Get(service.Name)
	if err != nil {
		klog.Infof("Service %s in namespace %s is deleted.", service.Name, service.Namespace)
		serviceDeleted = true
	} else {
		service = svc
	}

	infraExistingEpSlices, err := c.getInfraEPSFromInfraService(ctx, service)
	if err != nil {
		return err
	}

	// At this point we have the current state of the 3 main objects we are interested in:
	// 1. The Service in the infra cluster, the one created by the KubevirtCloudController.
	// 2. The EndpointSlices in the tenant cluster, created for the tenant cluster's Service.
	// 3. The EndpointSlices in the infra cluster, managed by this controller.

	slicesToDelete := []*discovery.EndpointSlice{}
	slicesByAddressType := make(map[discovery.AddressType][]*discovery.EndpointSlice)

	serviceSupportedAddressesTypes := getAddressTypesForService(service)
	// If the services switched to a different address type, we need to delete the old ones, because it's immutable.
	// If the services switched to a different externalTrafficPolicy, we need to delete the old ones.
	for _, eps := range infraExistingEpSlices {
		if service.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal || serviceDeleted {
			klog.Infof("Added for deletion EndpointSlice %s in namespace %s because it has an externalTrafficPolicy different from Local", eps.Name, eps.Namespace)
			// to be sure we don't delete any slice that is not managed by us
			if c.managedByController(eps) {
				slicesToDelete = append(slicesToDelete, eps)
			}
			continue
		}
		if !serviceSupportedAddressesTypes.Has(eps.AddressType) {
			klog.Infof("Added for deletion EndpointSlice %s in namespace %s because it has an unsupported address type: %v", eps.Name, eps.Namespace, eps.AddressType)
			slicesToDelete = append(slicesToDelete, eps)
			continue
		}
		slicesByAddressType[eps.AddressType] = append(slicesByAddressType[eps.AddressType], eps)
	}

	if !serviceDeleted {
		// Get tenant's endpoint slices for this service
		tenantEpSlices, err := c.getTenantEPSFromInfraService(ctx, service)
		if err != nil {
			return err
		}

		// Reconcile the EndpointSlices for each address type e.g. ipv4, ipv6
		for addressType := range serviceSupportedAddressesTypes {
			existingSlices := slicesByAddressType[addressType]
			err := c.reconcileByAddressType(service, tenantEpSlices, existingSlices, addressType)
			if err != nil {
				return err
			}
		}
	}

	// Delete the EndpointSlices that are no longer needed
	for _, eps := range slicesToDelete {
		err := c.infraClient.DiscoveryV1().EndpointSlices(eps.Namespace).Delete(context.TODO(), eps.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("Failed to delete EndpointSlice %s in namespace %s: %v", eps.Name, eps.Namespace, err)
			return err
		}
		klog.Infof("Deleted EndpointSlice %s in namespace %s", eps.Name, eps.Namespace)
	}

	return nil
}

//TODO: From here cleanup!

func (c *Controller) reconcileByAddressType(service *v1.Service, tenantSlices []*discovery.EndpointSlice, existingSlices []*discovery.EndpointSlice, addressType discovery.AddressType) error {

	slicesToCreate := []*discovery.EndpointSlice{}
	slicesToUpdate := []*discovery.EndpointSlice{}
	slicesToDelete := []*discovery.EndpointSlice{}
	slicesUntouched := []*discovery.EndpointSlice{}

	// Create the desired port configuration
	var desiredPorts []discovery.EndpointPort

	for _, port := range service.Spec.Ports {
		desiredPorts = append(desiredPorts, discovery.EndpointPort{
			Port:     &port.TargetPort.IntVal,
			Protocol: &port.Protocol,
			Name:     &port.Name,
		})
	}

	// Create the desired endpoint configuration
	var desiredEndpoints []*discovery.Endpoint
	desiredEndpoints = c.getDesiredEndpoints(service, tenantSlices)
	desiredEndpointSet := endpointsliceutil.EndpointSet{}
	desiredEndpointSet.Insert(desiredEndpoints...)

	//  1. Iterate through existing slices, delete endpoints that are no longer
	//     desired and update matching endpoints that have changed. It also checks
	//     if the slices have the labels of the parent services, and updates them if not.
	for _, existingSlice := range existingSlices {
		var coveredEndpoints []discovery.Endpoint
		sliceUpdated := false
		// first enforce the right portmapping
		if !apiequality.Semantic.DeepEqual(existingSlice.Ports, desiredPorts) {
			existingSlice.Ports = desiredPorts
			sliceUpdated = true
		}
		for _, endpoint := range existingSlice.Endpoints {
			present := desiredEndpointSet.Get(&endpoint)
			if present != nil {
				// one of the desired endpoint is covered by this slice
				coveredEndpoints = append(coveredEndpoints, *present)
				// Check if the endpoint needs updating
				if !endpointsliceutil.EndpointsEqualBeyondHash(present, &endpoint) {
					sliceUpdated = true
				}
				// remove endpoint from desired set because it's already covered.
				desiredEndpointSet.Delete(&endpoint)
			}
		}
		// Check if the labels need updating
		labels, labelsChanged := c.ensureEndpointSliceLabels(existingSlice, service)

		// If an endpoint was updated or removed, mark for update or delete
		if sliceUpdated || labelsChanged || len(existingSlice.Endpoints) != len(coveredEndpoints) {
			if len(coveredEndpoints) == 0 {
				// No endpoint that is desired is covered by this slice, so it should be deleted
				slicesToDelete = append(slicesToDelete, existingSlice)
			} else {
				// Here we override the existing endpoints with the covered ones
				// This also deletes the unwanted endpoints from the existing slice
				existingSlice.Endpoints = coveredEndpoints
				existingSlice.Labels = labels
				slicesToUpdate = append(slicesToUpdate, existingSlice)
			}
		} else {
			slicesUntouched = append(slicesUntouched, existingSlice)
		}
	}
	//  2. Iterate through slices that have been modified in 1 and fill them up with
	//     any remaining desired endpoints.
	// FillAlreadyUpdatedSlicesWithDesiredEndpoints
	if desiredEndpointSet.Len() > 0 {
		for _, existingUpdatedSlice := range slicesToUpdate {
			for len(existingUpdatedSlice.Endpoints) < c.maxEndPointsPerSlice {
				endpoint, ok := desiredEndpointSet.PopAny()
				if !ok {
					break
				}
				existingUpdatedSlice.Endpoints = append(existingUpdatedSlice.Endpoints, *endpoint)
			}
		}
	}

	//  3. If there still desired endpoints left, try to fit them into a previously
	//     unchanged slice and/or create new ones.
	// FillUntouchedSlicesWithDesiredEndpoints
	if desiredEndpointSet.Len() > 0 {
		for _, untouchedSlice := range slicesUntouched {
			for len(untouchedSlice.Endpoints) < c.maxEndPointsPerSlice {
				endpoint, ok := desiredEndpointSet.PopAny()
				if !ok {
					break
				}
				untouchedSlice.Endpoints = append(untouchedSlice.Endpoints, *endpoint)
			}
			slicesToUpdate = append(slicesToUpdate, untouchedSlice)
		}
	}

	//  4. If there still desired endpoints left, create new slices.
	if desiredEndpointSet.Len() > 0 {
		slice := c.newSlice(service, desiredPorts, addressType)
		slice.Labels, _ = c.ensureEndpointSliceLabels(slice, service)
		for len(slice.Endpoints) < c.maxEndPointsPerSlice {
			endpoint, ok := desiredEndpointSet.PopAny()
			if !ok {
				break
			}
			slice.Endpoints = append(slice.Endpoints, *endpoint)
		}
		slicesToCreate = append(slicesToCreate, slice)
	}

	return c.finalize(service, slicesToCreate, slicesToUpdate, slicesToDelete)
}

func ownedBy(endpointSlice *discovery.EndpointSlice, svc *v1.Service) bool {
	for _, o := range endpointSlice.OwnerReferences {
		if o.UID == svc.UID && o.Kind == "Service" && o.APIVersion == "v1" {
			return true
		}
	}
	return false
}

func (c *Controller) finalize(service *v1.Service, slicesToCreate []*discovery.EndpointSlice, slicesToUpdate []*discovery.EndpointSlice, slicesToDelete []*discovery.EndpointSlice) error {
	// If there are slices to delete and slices to create, make them as update
	for i := 0; i < len(slicesToDelete); {
		if len(slicesToCreate) == 0 {
			break
		}
		if slicesToDelete[i].AddressType == slicesToCreate[0].AddressType && ownedBy(slicesToDelete[i], service) {
			slicesToCreate[0].Name = slicesToDelete[i].Name
			slicesToCreate = slicesToCreate[1:]
			slicesToUpdate = append(slicesToUpdate, slicesToCreate[0])
			slicesToDelete = append(slicesToDelete[:i], slicesToDelete[i+1:]...)
		} else {
			i++
		}
	}

	// Create the new slices if service is not marked for deletion
	if service.DeletionTimestamp == nil {
		for _, slice := range slicesToCreate {
			createdSlice, err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Create(context.TODO(), slice, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("Failed to create EndpointSlice %s in namespace %s: %v", slice.Name, slice.Namespace, err)
				if k8serrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					return nil
				}
				return err
			}
			klog.Infof("Created EndpointSlice %s in namespace %s", createdSlice.Name, createdSlice.Namespace)
		}
	}

	// Update slices
	for _, slice := range slicesToUpdate {
		_, err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Update(context.TODO(), slice, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Failed to update EndpointSlice %s in namespace %s: %v", slice.Name, slice.Namespace, err)
			return err
		}
		klog.Infof("Updated EndpointSlice %s in namespace %s", slice.Name, slice.Namespace)
	}

	// Delete slices
	for _, slice := range slicesToDelete {
		err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Delete(context.TODO(), slice.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("Failed to delete EndpointSlice %s in namespace %s: %v", slice.Name, slice.Namespace, err)
			return err
		}
		klog.Infof("Deleted EndpointSlice %s in namespace %s", slice.Name, slice.Namespace)
	}

	return nil
}

func (c *Controller) newSlice(service *v1.Service, desiredPorts []discovery.EndpointPort, addressType discovery.AddressType) *discovery.EndpointSlice {
	ownerRef := metav1.NewControllerRef(service, schema.GroupVersionKind{Version: "v1", Kind: "Service"})

	slice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Labels:          map[string]string{},
			GenerateName:    service.Name,
			Namespace:       service.Namespace,
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		Ports:       desiredPorts,
		AddressType: addressType,
		Endpoints:   []discovery.Endpoint{},
	}
	return slice
}

func (c *Controller) getDesiredEndpoints(service *v1.Service, tenantSlices []*discovery.EndpointSlice) []*discovery.Endpoint {
	var desiredEndpoints []*discovery.Endpoint
	if service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
		// Extract the desired endpoints from the tenant EndpointSlices
		// for extracting the nodes it does not matter what type of address we are dealing with
		// all nodes with an endpoint for a corresponding slice will be selected.
		nodeSet := sets.Set[string]{}
		for _, slice := range tenantSlices {
			for _, endpoint := range slice.Endpoints {
				// find all unique nodes that correspond to an endpoint in a tenant slice
				nodeSet.Insert(*endpoint.NodeName)
			}
		}

		klog.Infof("Desired nodes for service %s in namespace %s: %v", service.Name, service.Namespace, sets.List(nodeSet))

		for _, node := range sets.List(nodeSet) {
			// find vmi for node name
			obj := &unstructured.Unstructured{}
			vmi := &kubevirtv1.VirtualMachineInstance{}

			obj, err := c.infraDynamic.Resource(kubevirtv1.VirtualMachineInstanceGroupVersionKind.GroupVersion().WithResource("virtualmachineinstances")).Namespace(c.infraNamespace).Get(context.TODO(), node, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Failed to get VirtualMachineInstance %s in namespace %s:%v", node, c.infraNamespace, err)
				continue
			}

			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, vmi)
			if err != nil {
				klog.Errorf("Failed to convert Unstructured to VirtualMachineInstance: %v", err)
				klog.Fatal(err)
			}

			ready := vmi.Status.Phase == kubevirtv1.Running
			serving := vmi.Status.Phase == kubevirtv1.Running
			terminating := vmi.Status.Phase == kubevirtv1.Failed || vmi.Status.Phase == kubevirtv1.Succeeded

			for _, i := range vmi.Status.Interfaces {
				if i.Name == "default" {
					desiredEndpoints = append(desiredEndpoints, &discovery.Endpoint{
						Addresses: []string{i.IP},
						Conditions: discovery.EndpointConditions{
							Ready:       &ready,
							Serving:     &serving,
							Terminating: &terminating,
						},
						NodeName: &vmi.Status.NodeName,
					})
					continue
				}
			}
		}
	}

	return desiredEndpoints
}

func (c *Controller) ensureEndpointSliceLabels(slice *discovery.EndpointSlice, svc *v1.Service) (map[string]string, bool) {
	labels := make(map[string]string)
	labelsChanged := false

	for k, v := range slice.Labels {
		labels[k] = v
	}

	for k, v := range svc.ObjectMeta.Labels {
		labels[k] = v
	}

	labels[discovery.LabelServiceName] = svc.Name
	labels[discovery.LabelManagedBy] = ControllerName.dashed()
	if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == v1.ClusterIPNone {
		labels[v1.IsHeadlessService] = ""
	} else {
		delete(labels, v1.IsHeadlessService)
	}

	if !apiequality.Semantic.DeepEqual(slice.Labels, labels) {
		labelsChanged = true
	}
	return labels, labelsChanged
}

func (c *Controller) managedByController(slice *discovery.EndpointSlice) bool {
	return slice.Labels[discovery.LabelManagedBy] == ControllerName.dashed()
}
