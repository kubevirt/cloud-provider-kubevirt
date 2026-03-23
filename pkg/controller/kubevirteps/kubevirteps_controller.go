package kubevirteps

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
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

	infraClient        kubernetes.Interface
	infraDynamic       dynamic.Interface
	infraFactory       informers.SharedInformerFactory
	infraDynamicFactory dynamicinformer.DynamicSharedInformerFactory

	infraNamespace string
	queue          workqueue.RateLimitingInterface
	maxRetries     int

	maxEndPointsPerSlice int

	// serviceVMITracker tracks which services depend on which VMIs
	serviceVMITracker *serviceVMITracker

	// vmiCache stores VMI data to detect state changes
	vmiCacheMu sync.RWMutex
	vmiCache   map[string]*vmiCacheEntry

	// periodicResyncInterval is the interval for periodic full reconciliation
	periodicResyncInterval time.Duration
}

// vmiCacheEntry stores cached VMI state to detect changes
type vmiCacheEntry struct {
	Phase      kubevirtv1.VirtualMachineInstancePhase
	NodeName   string
	Interfaces []kubevirtv1.VirtualMachineInstanceNetworkInterface
	// MigrationState tracks if VMI is being migrated
	MigrationState *kubevirtv1.VirtualMachineInstanceMigrationState
}

// DefaultPeriodicResyncInterval is the default interval for periodic full reconciliation
const DefaultPeriodicResyncInterval = 30 * time.Second

func NewKubevirtEPSController(
	tenantClient kubernetes.Interface,
	infraClient kubernetes.Interface,
	infraDynamic dynamic.Interface,
	infraNamespace string) *Controller {

	tenantFactory := informers.NewSharedInformerFactory(tenantClient, 0)
	infraFactory := informers.NewSharedInformerFactoryWithOptions(infraClient, 0, informers.WithNamespace(infraNamespace))
	// Create dynamic informer factory for VMI watching
	infraDynamicFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		infraDynamic, 0, infraNamespace, nil)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	return &Controller{
		tenantClient:           tenantClient,
		tenantFactory:          tenantFactory,
		tenantEPSTracker:       tenantEPSTracker{},
		infraClient:            infraClient,
		infraDynamic:           infraDynamic,
		infraFactory:           infraFactory,
		infraDynamicFactory:    infraDynamicFactory,
		infraNamespace:         infraNamespace,
		queue:                  queue,
		maxRetries:             25,
		maxEndPointsPerSlice:   100,
		serviceVMITracker:      newServiceVMITracker(),
		vmiCache:               make(map[string]*vmiCacheEntry),
		periodicResyncInterval: DefaultPeriodicResyncInterval,
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

	// Add a dynamic informer for VirtualMachineInstances in the infra cluster
	// This is the KEY FIX: watch VMI changes to trigger reconciliation on live migration,
	// start, stop, restart, and other state changes
	vmiGVR := schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachineinstances",
	}
	_, err = c.infraDynamicFactory.ForResource(vmiGVR).Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleVMIEvent(obj, nil)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.handleVMIEvent(newObj, oldObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.handleVMIEvent(nil, obj)
		},
	})
	if err != nil {
		return err
	}

	return nil
}

// handleVMIEvent processes VMI add/update/delete events and triggers reconciliation
// for affected services when relevant VMI state changes occur.
func (c *Controller) handleVMIEvent(newObj, oldObj interface{}) {
	var vmi *kubevirtv1.VirtualMachineInstance
	var oldVMI *kubevirtv1.VirtualMachineInstance
	var vmiName string

	// Handle delete case
	if newObj == nil && oldObj != nil {
		oldUnstructured, ok := oldObj.(*unstructured.Unstructured)
		if !ok {
			// Handle DeletedFinalStateUnknown
			if deletedState, ok := oldObj.(cache.DeletedFinalStateUnknown); ok {
				oldUnstructured, ok = deletedState.Obj.(*unstructured.Unstructured)
				if !ok {
					klog.Errorf("Failed to cast deleted VMI object")
					return
				}
			} else {
				klog.Errorf("Failed to cast old VMI object")
				return
			}
		}
		vmiName = oldUnstructured.GetName()
		klog.Infof("VMI deleted: %s, triggering reconciliation for affected services", vmiName)

		// Remove from cache
		c.vmiCacheMu.Lock()
		delete(c.vmiCache, vmiName)
		c.vmiCacheMu.Unlock()

		// Queue all services that depended on this VMI
		c.queueServicesForVMI(vmiName)
		return
	}

	// Handle add/update case
	newUnstructured, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		klog.Errorf("Failed to cast new VMI object")
		return
	}

	vmi = &kubevirtv1.VirtualMachineInstance{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(newUnstructured.Object, vmi); err != nil {
		klog.Errorf("Failed to convert Unstructured to VirtualMachineInstance: %v", err)
		return
	}
	vmiName = vmi.Name

	// Convert old object if present (update case)
	if oldObj != nil {
		oldUnstructured, ok := oldObj.(*unstructured.Unstructured)
		if ok {
			oldVMI = &kubevirtv1.VirtualMachineInstance{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(oldUnstructured.Object, oldVMI); err != nil {
				klog.V(4).Infof("Failed to convert old Unstructured to VirtualMachineInstance: %v", err)
				oldVMI = nil
			}
		}
	}

	// Check if this is a relevant state change
	if c.isRelevantVMIChange(vmi, oldVMI) {
		klog.Infof("VMI %s state changed (phase=%s, node=%s, migrating=%v), triggering reconciliation",
			vmiName, vmi.Status.Phase, vmi.Status.NodeName, c.isVMIMigrating(vmi))

		// Update cache
		c.updateVMICache(vmiName, vmi)

		// Queue all services that depend on this VMI
		c.queueServicesForVMI(vmiName)
	}
}

// isRelevantVMIChange checks if the VMI change is relevant for endpoint reconciliation.
// Relevant changes include: Phase, NodeName, Interfaces, MigrationState
func (c *Controller) isRelevantVMIChange(newVMI, oldVMI *kubevirtv1.VirtualMachineInstance) bool {
	// Always process if there's no old VMI (new VMI added)
	if oldVMI == nil {
		return true
	}

	// Check phase change
	if newVMI.Status.Phase != oldVMI.Status.Phase {
		klog.V(4).Infof("VMI %s phase changed: %s -> %s", newVMI.Name, oldVMI.Status.Phase, newVMI.Status.Phase)
		return true
	}

	// Check node name change (migration completed)
	if newVMI.Status.NodeName != oldVMI.Status.NodeName {
		klog.V(4).Infof("VMI %s node changed: %s -> %s", newVMI.Name, oldVMI.Status.NodeName, newVMI.Status.NodeName)
		return true
	}

	// Check migration state change
	oldMigrating := oldVMI.Status.MigrationState != nil
	newMigrating := newVMI.Status.MigrationState != nil
	if oldMigrating != newMigrating {
		klog.V(4).Infof("VMI %s migration state changed: migrating=%v -> %v", newVMI.Name, oldMigrating, newMigrating)
		return true
	}

	// Check if migration completed or failed
	if newMigrating && oldMigrating {
		if !reflect.DeepEqual(newVMI.Status.MigrationState, oldVMI.Status.MigrationState) {
			klog.V(4).Infof("VMI %s migration state details changed", newVMI.Name)
			return true
		}
	}

	// Check interface IP changes
	if !reflect.DeepEqual(newVMI.Status.Interfaces, oldVMI.Status.Interfaces) {
		klog.V(4).Infof("VMI %s interfaces changed", newVMI.Name)
		return true
	}

	return false
}

// updateVMICache updates the VMI cache with the current state
func (c *Controller) updateVMICache(vmiName string, vmi *kubevirtv1.VirtualMachineInstance) {
	c.vmiCacheMu.Lock()
	defer c.vmiCacheMu.Unlock()

	c.vmiCache[vmiName] = &vmiCacheEntry{
		Phase:          vmi.Status.Phase,
		NodeName:       vmi.Status.NodeName,
		Interfaces:     vmi.Status.Interfaces,
		MigrationState: vmi.Status.MigrationState,
	}
}

// queueServicesForVMI queues all services that depend on the given VMI for reconciliation
func (c *Controller) queueServicesForVMI(vmiName string) {
	services := c.serviceVMITracker.getServicesForVMI(vmiName)
	if len(services) == 0 {
		klog.V(4).Infof("No services found for VMI %s", vmiName)
		return
	}

	for _, svcName := range services {
		svc, err := c.infraFactory.Core().V1().Services().Lister().Services(c.infraNamespace).Get(svcName)
		if err != nil {
			klog.V(4).Infof("Failed to get service %s for VMI %s: %v", svcName, vmiName, err)
			continue
		}
		klog.Infof("Queuing service %s for reconciliation due to VMI %s change", svcName, vmiName)
		c.queue.Add(newRequest(UpdateReq, svc, nil))
	}
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

	klog.Infof("Starting %s", ControllerName)
	defer klog.Infof("Shutting down %s", ControllerName)
	controllerManagerMetrics.ControllerStarted(ControllerName.String())
	defer controllerManagerMetrics.ControllerStopped(ControllerName.String())

	c.tenantFactory.Start(stopCh)
	c.infraFactory.Start(stopCh)
	c.infraDynamicFactory.Start(stopCh)

	// Get the VMI GVR for cache sync check
	vmiGVR := schema.GroupVersionResource{
		Group:    "kubevirt.io",
		Version:  "v1",
		Resource: "virtualmachineinstances",
	}

	if !cache.WaitForNamedCacheSync(ControllerName.String(), stopCh,
		c.infraFactory.Core().V1().Services().Informer().HasSynced,
		c.tenantFactory.Discovery().V1().EndpointSlices().Informer().HasSynced,
		c.infraDynamicFactory.ForResource(vmiGVR).Informer().HasSynced) {
		return
	}

	klog.Infof("All informers synced, starting workers and periodic resync")

	for i := 0; i < numWorkers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	// Start periodic full reconciliation as a safety net
	go c.runPeriodicResync(ctx, stopCh)

	<-stopCh
}

// runPeriodicResync periodically triggers a full reconciliation of all tracked services.
// This acts as a safety net to catch any missed events or state inconsistencies.
func (c *Controller) runPeriodicResync(ctx context.Context, stopCh <-chan struct{}) {
	ticker := time.NewTicker(c.periodicResyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			c.triggerFullResync()
		}
	}
}

// triggerFullResync queues all tracked services for reconciliation
func (c *Controller) triggerFullResync() {
	klog.V(4).Infof("Triggering periodic full resync of all tracked services")

	services := c.serviceVMITracker.getAllServices()
	for _, svcName := range services {
		svc, err := c.infraFactory.Core().V1().Services().Lister().Services(c.infraNamespace).Get(svcName)
		if err != nil {
			klog.V(4).Infof("Failed to get service %s during periodic resync: %v", svcName, err)
			continue
		}
		// Use a lower priority for periodic resyncs by checking if already queued
		c.queue.Add(newRequest(UpdateReq, svc, nil))
	}
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
		// Clean up service-VMI mappings for deleted service
		c.serviceVMITracker.removeService(service.Name)
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
		if service.Spec.Selector != nil || serviceDeleted {
			klog.Infof("Added for deletion EndpointSlice %s in namespace %s because it has a selector", eps.Name, eps.Namespace)
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
	var vmiNames []string // Track VMIs this service depends on

	// When service has a selector (Cluster traffic policy), this controller doesn't manage endpoints.
	// Clear any stale VMI mappings for this service.
	if service.Spec.Selector != nil {
		c.serviceVMITracker.removeService(service.Name)
		return desiredEndpoints
	}

	if service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal || service.Spec.Selector == nil {
		// Extract the desired endpoints from the tenant EndpointSlices.
		// Track per-node readiness: a node is considered ready if at least one
		// tenant endpoint on it is ready. This ensures that when a node reboots
		// (guest OS restart inside a KubeVirt VM), the infra endpoint is marked
		// not-ready even though the VMI stays Running.
		nodeReady := map[string]bool{}
		for _, slice := range tenantSlices {
			for _, endpoint := range slice.Endpoints {
				if endpoint.NodeName == nil {
					continue
				}
				node := *endpoint.NodeName
				epReady := endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
				if epReady {
					nodeReady[node] = true
				} else if _, exists := nodeReady[node]; !exists {
					nodeReady[node] = false
				}
			}
		}

		nodeNames := make([]string, 0, len(nodeReady))
		for n := range nodeReady {
			nodeNames = append(nodeNames, n)
		}
		sort.Strings(nodeNames)
		klog.Infof("Desired nodes for service %s in namespace %s: %v", service.Name, service.Namespace, nodeNames)

		for _, node := range nodeNames {
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

			// Track this VMI for service-VMI mapping
			vmiNames = append(vmiNames, vmi.Name)

			// Update VMI cache
			c.updateVMICache(vmi.Name, vmi)

			// Handle transient migration state
			// During migration, mark endpoint as not ready to prevent traffic to potentially stale IP
			isMigrating := c.isVMIMigrating(vmi)
			if isMigrating {
				klog.Infof("VMI %s is currently migrating, marking endpoint as not ready", vmi.Name)
			}

			// Combine VMI readiness with tenant endpoint readiness.
			// When a node reboots inside a KubeVirt VM, the VMI stays Running
			// but the tenant endpoint controller marks pods on that node as not-ready.
			// We must respect that signal to avoid routing traffic to a dead node.
			tenantReady := nodeReady[node]
			if !tenantReady {
				klog.Infof("Node %s has no ready tenant endpoints, marking infra endpoint as not ready", node)
			}

			ready := vmi.Status.Phase == kubevirtv1.Running && !isMigrating && tenantReady
			serving := vmi.Status.Phase == kubevirtv1.Running && !isMigrating && tenantReady
			terminating := vmi.Status.Phase == kubevirtv1.Failed || vmi.Status.Phase == kubevirtv1.Succeeded

			// choose "pod" if present, else "default"
			var chosen *kubevirtv1.VirtualMachineInstanceNetworkInterface
			for idx := range vmi.Status.Interfaces {
				if vmi.Status.Interfaces[idx].Name == "pod" {
					chosen = &vmi.Status.Interfaces[idx]
					break
				}
				if vmi.Status.Interfaces[idx].Name == "default" && chosen == nil {
					chosen = &vmi.Status.Interfaces[idx]
				}
			}
			if chosen == nil {
				klog.V(4).Infof("VMI %s has no suitable network interface", vmi.Name)
				continue
			}
			ip := chosen.IP
			if ip == "" && len(chosen.IPs) > 0 {
				ip = chosen.IPs[0]
			}
			if ip == "" {
				klog.V(4).Infof("VMI %s has no IP address on interface %s", vmi.Name, chosen.Name)
				continue
			}

			desiredEndpoints = append(desiredEndpoints, &discovery.Endpoint{
				Addresses: []string{ip},
				Conditions: discovery.EndpointConditions{
					Ready:       &ready,
					Serving:     &serving,
					Terminating: &terminating,
				},
				NodeName: &vmi.Status.NodeName,
			})
		}
	}

	// Update service-VMI mapping for future VMI event handling
	c.serviceVMITracker.updateServiceVMIs(service.Name, vmiNames)

	return desiredEndpoints
}

// isVMIMigrating checks if a VMI is currently in a migration state
func (c *Controller) isVMIMigrating(vmi *kubevirtv1.VirtualMachineInstance) bool {
	if vmi.Status.MigrationState == nil {
		return false
	}

	// Check if migration is in progress (not completed and not failed)
	migrationState := vmi.Status.MigrationState
	if migrationState.Completed || migrationState.Failed {
		return false
	}

	// Migration is in progress
	klog.V(4).Infof("VMI %s migration in progress: startTimestamp=%v, targetNode=%s, sourceNode=%s",
		vmi.Name,
		migrationState.StartTimestamp,
		migrationState.TargetNode,
		migrationState.SourceNode)

	return true
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
