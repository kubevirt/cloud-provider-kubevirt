package kubevirteps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	// Existing Service event handlers...
	_, err := c.infraFactory.Core().V1().Services().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*v1.Service)
			if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service added: %v/%v", svc.Namespace, svc.Name)
				c.queue.Add(newRequest(AddReq, obj, nil))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newSvc := newObj.(*v1.Service)
			if newSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service updated: %v/%v", newSvc.Namespace, newSvc.Name)
				c.queue.Add(newRequest(UpdateReq, newObj, oldObj))
			}
		},
		DeleteFunc: func(obj interface{}) {
			svc := obj.(*v1.Service)
			if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
				klog.Infof("Service deleted: %v/%v", svc.Namespace, svc.Name)
				c.queue.Add(newRequest(DeleteReq, obj, nil))
			}
		},
	})
	if err != nil {
		return err
	}

	// Existing EndpointSlice event handlers in tenant cluster...
	_, err = c.tenantFactory.Discovery().V1().EndpointSlices().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			eps := obj.(*discovery.EndpointSlice)
			if c.tenantEPSTracker.contains(eps) {
				klog.Infof("get Infra Service for Tenant EndpointSlice: %v/%v", eps.Namespace, eps.Name)
				var infraSvc *v1.Service
				infraSvc, err = c.getInfraServiceFromTenantEPS(context.TODO(), eps)
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
				var infraSvc *v1.Service
				infraSvc, err = c.getInfraServiceFromTenantEPS(context.TODO(), eps)
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
				var infraSvc *v1.Service
				infraSvc, err = c.getInfraServiceFromTenantEPS(context.TODO(), eps)
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

	// Add an informer for EndpointSlices in the infra cluster
	_, err = c.infraFactory.Discovery().V1().EndpointSlices().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			eps := obj.(*discovery.EndpointSlice)
			if c.managedByController(eps) {
				svc, svcErr := c.getInfraServiceForEPS(context.TODO(), eps)
				if svcErr != nil {
					klog.Errorf("Failed to get infra Service for EndpointSlice %s/%s: %v", eps.Namespace, eps.Name, svcErr)
					return
				}
				if svc != nil {
					klog.Infof("Infra EndpointSlice added: %v/%v, requeuing Service: %v/%v", eps.Namespace, eps.Name, svc.Namespace, svc.Name)
					c.queue.Add(newRequest(AddReq, svc, nil))
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			eps := newObj.(*discovery.EndpointSlice)
			if c.managedByController(eps) {
				svc, svcErr := c.getInfraServiceForEPS(context.TODO(), eps)
				if svcErr != nil {
					klog.Errorf("Failed to get infra Service for EndpointSlice %s/%s: %v", eps.Namespace, eps.Name, svcErr)
					return
				}
				if svc != nil {
					klog.Infof("Infra EndpointSlice updated: %v/%v, requeuing Service: %v/%v", eps.Namespace, eps.Name, svc.Namespace, svc.Name)
					c.queue.Add(newRequest(UpdateReq, svc, nil))
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			eps := obj.(*discovery.EndpointSlice)
			if c.managedByController(eps) {
				svc, svcErr := c.getInfraServiceForEPS(context.TODO(), eps)
				if svcErr != nil {
					klog.Errorf("Failed to get infra Service for EndpointSlice %s/%s on delete: %v", eps.Namespace, eps.Name, svcErr)
					return
				}
				if svc != nil {
					klog.Infof("Infra EndpointSlice deleted: %v/%v, requeuing Service: %v/%v", eps.Namespace, eps.Name, svc.Namespace, svc.Name)
					c.queue.Add(newRequest(DeleteReq, svc, nil))
				}
			}
		},
	})
	if err != nil {
		return err
	}

	return nil
}

// getInfraServiceForEPS returns the Service in the infra cluster associated with the given EndpointSlice.
// It does this by reading the "kubernetes.io/service-name" label from the EndpointSlice, which should correspond
// to the Service name. If not found or if the Service doesn't exist, it returns nil.
func (c *Controller) getInfraServiceForEPS(ctx context.Context, eps *discovery.EndpointSlice) (*v1.Service, error) {
	svcName := eps.Labels[discovery.LabelServiceName]
	if svcName == "" {
		// No service name label found, can't determine infra service.
		return nil, nil
	}

	svc, err := c.infraClient.CoreV1().Services(c.infraNamespace).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Service doesn't exist
			return nil, nil
		}
		return nil, err
	}

	return svc, nil
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
	if !ok || service == nil {
		return errors.New("could not cast object to service")
	}

	/*
	   Skip if the given Service is not labeled with the keys that indicate
	   it was created/managed by this controller (i.e., not a LoadBalancer
	   that we handle).
	*/
	if service.Labels[kubevirt.TenantServiceNameLabelKey] == "" ||
		service.Labels[kubevirt.TenantServiceNamespaceLabelKey] == "" ||
		service.Labels[kubevirt.TenantClusterNameLabelKey] == "" {
		klog.Infof("This LoadBalancer Service: %s is not managed by the %s. Skipping.", service.Name, ControllerName)
		return nil
	}

	klog.Infof("Reconciling: %v", service.Name)

	/*
	   1) Check if Service in the infra cluster is actually present.
	      If it's not found, mark it as 'deleted' so that we don't create new slices.
	*/
	serviceDeleted := false
	infraSvc, err := c.infraFactory.Core().V1().Services().Lister().Services(c.infraNamespace).Get(service.Name)
	if err != nil {
		// The Service is not present in the infra lister => treat as deleted
		klog.Infof("Service %s in namespace %s is deleted (or not found).", service.Name, service.Namespace)
		serviceDeleted = true
	} else {
		// Use the actual object from the lister, so we have the latest state
		service = infraSvc
	}

	/*
	   2) Get all existing EndpointSlices in the infra cluster that belong to this LB Service.
	      We'll decide which of them should be updated or deleted.
	*/
	infraExistingEpSlices, err := c.getInfraEPSFromInfraService(ctx, service)
	if err != nil {
		return err
	}

	slicesToDelete := []*discovery.EndpointSlice{}
	slicesByAddressType := make(map[discovery.AddressType][]*discovery.EndpointSlice)

	// For example, if the service is single-stack IPv4 => only AddressTypeIPv4
	// or if dual-stack => IPv4 and IPv6, etc.
	serviceSupportedAddressesTypes := getAddressTypesForService(service)

	/*
	   3) Determine which slices to delete, and which to pass on to the normal
	      "reconcileByAddressType" logic.

	      - If 'serviceDeleted' is true OR service.Spec.Selector != nil, we remove them.
	      - Also, if the slice's address type is unsupported by the Service, we remove it.
	*/
	for _, eps := range infraExistingEpSlices {
		// If service is deleted or has a non-nil selector => remove slices
		if serviceDeleted || service.Spec.Selector != nil {
			/*
			   Only remove if it is clearly labeled as managed by us:
			   we do not want to accidentally remove slices that are not
			   created by this controller.
			*/
			if c.managedByController(eps) {
				klog.Infof("Added for deletion EndpointSlice %s in namespace %s because service is deleted or has a selector",
					eps.Name, eps.Namespace)
				slicesToDelete = append(slicesToDelete, eps)
			}
			continue
		}

		// If the Service does not support this slice's AddressType => remove
		if !serviceSupportedAddressesTypes.Has(eps.AddressType) {
			klog.Infof("Added for deletion EndpointSlice %s in namespace %s because it has an unsupported address type: %v",
				eps.Name, eps.Namespace, eps.AddressType)
			slicesToDelete = append(slicesToDelete, eps)
			continue
		}

		/*
		   Otherwise, this slice is potentially still valid for the given AddressType,
		   we'll send it to reconcileByAddressType for final merging and updates.
		*/
		slicesByAddressType[eps.AddressType] = append(slicesByAddressType[eps.AddressType], eps)
	}

	/*
	   4) If the Service was NOT deleted and has NO selector (i.e., it's a "no-selector" LB Service),
	      we proceed to handle creation and updates. That means:
	      - Gather Tenant's EndpointSlices
	      - Reconcile them by each AddressType
	*/
	if !serviceDeleted && service.Spec.Selector == nil {
		tenantEpSlices, err := c.getTenantEPSFromInfraService(ctx, service)
		if err != nil {
			return err
		}

		// For each addressType (ipv4, ipv6, etc.) reconcile the infra slices
		for addressType := range serviceSupportedAddressesTypes {
			existingSlices := slicesByAddressType[addressType]
			if err := c.reconcileByAddressType(service, tenantEpSlices, existingSlices, addressType); err != nil {
				return err
			}
		}
	}

	/*
	   5) Perform the actual deletion of all slices we flagged.
	      In many cases (serviceDeleted or .Spec.Selector != nil),
	      we end up with only "delete" actions and no new slice creation.
	*/
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

	for i := range service.Spec.Ports {
		desiredPorts = append(desiredPorts, discovery.EndpointPort{
			Port:     &service.Spec.Ports[i].TargetPort.IntVal,
			Protocol: &service.Spec.Ports[i].Protocol,
			Name:     &service.Spec.Ports[i].Name,
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

func (c *Controller) finalize(
	service *v1.Service,
	slicesToCreate []*discovery.EndpointSlice,
	slicesToUpdate []*discovery.EndpointSlice,
	slicesToDelete []*discovery.EndpointSlice,
) error {
	/*
	   We try to turn a "delete + create" pair into a single "update" operation
	   if the original slice (slicesToDelete[i]) has the same address type as
	   the first slice in slicesToCreate, and is owned by the same Service.

	   However, we must re-check the lengths of slicesToDelete and slicesToCreate
	   within the loop to avoid an out-of-bounds index in slicesToCreate.
	*/

	i := 0
	for i < len(slicesToDelete) {
		// If there is nothing to create, break early
		if len(slicesToCreate) == 0 {
			break
		}

		sd := slicesToDelete[i]
		sc := slicesToCreate[0] // We can safely do this now, because len(slicesToCreate) > 0

		// If the address type matches, and the slice is owned by the same Service,
		// then instead of deleting sd and creating sc, we'll transform it into an update:
		// we rename sc with sd's name, remove sd from the delete list, remove sc from the create list,
		// and add sc to the update list.
		if sd.AddressType == sc.AddressType && ownedBy(sd, service) {
			sliceToUpdate := sc
			sliceToUpdate.Name = sd.Name

			// Remove the first element from slicesToCreate
			slicesToCreate = slicesToCreate[1:]

			// Remove the slice from slicesToDelete
			slicesToDelete = append(slicesToDelete[:i], slicesToDelete[i+1:]...)

			// Now add the renamed slice to the list of slices we want to update
			slicesToUpdate = append(slicesToUpdate, sliceToUpdate)

			/*
			   Do not increment i here, because we've just removed an element from
			   slicesToDelete. The next slice to examine is now at the same index i.
			*/
		} else {
			// If they don't match, move on to the next slice in slicesToDelete.
			i++
		}
	}

	/*
	   If the Service is not being deleted, create all remaining slices in slicesToCreate.
	   (If the Service has a DeletionTimestamp, it means it is going away, so we do not
	   want to create new EndpointSlices.)
	*/
	if service.DeletionTimestamp == nil {
		for _, slice := range slicesToCreate {
			createdSlice, err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Create(
				context.TODO(),
				slice,
				metav1.CreateOptions{},
			)
			if err != nil {
				klog.Errorf("Failed to create EndpointSlice %s in namespace %s: %v",
					slice.Name, slice.Namespace, err)
				// If the namespace is terminating, it's safe to ignore the error.
				if k8serrors.HasStatusCause(err, v1.NamespaceTerminatingCause) {
					continue
				}
				return err
			}
			klog.Infof("Created EndpointSlice %s in namespace %s",
				createdSlice.Name, createdSlice.Namespace)
		}
	}

	// Update slices that are in the slicesToUpdate list.
	for _, slice := range slicesToUpdate {
		_, err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Update(
			context.TODO(),
			slice,
			metav1.UpdateOptions{},
		)
		if err != nil {
			klog.Errorf("Failed to update EndpointSlice %s in namespace %s: %v",
				slice.Name, slice.Namespace, err)
			return err
		}
		klog.Infof("Updated EndpointSlice %s in namespace %s",
			slice.Name, slice.Namespace)
	}

	// Finally, delete slices that are in slicesToDelete and are no longer needed.
	for _, slice := range slicesToDelete {
		err := c.infraClient.DiscoveryV1().EndpointSlices(slice.Namespace).Delete(
			context.TODO(),
			slice.Name,
			metav1.DeleteOptions{},
		)
		if err != nil {
			klog.Errorf("Failed to delete EndpointSlice %s in namespace %s: %v",
				slice.Name, slice.Namespace, err)
			return err
		}
		klog.Infof("Deleted EndpointSlice %s in namespace %s",
			slice.Name, slice.Namespace)
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
	if service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal || service.Spec.Selector == nil {
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
