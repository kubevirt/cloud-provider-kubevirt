package cloud_provider_kubevirt

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/cloud-provider-kubevirt/test/e2e/naming"
	"kubevirt.io/cloud-provider-kubevirt/test/e2e/resource"
	"kubevirt.io/cloud-provider-kubevirt/test/resources"
)

const namespace = "default"
const testAppName = "test-app"

var _ = Describe("Load Balancer", func() {
	var (
		err                   error
		server                *appsv1.Deployment
		service               *v1.Service
		curlJob               *batchv1.Job
		backgroundPropagation = metav1.DeletePropagationBackground
	)

	BeforeEach(func() {
		server = resources.HTTPServerDeployment(testAppName, namespace)
		service = resources.HTTPServerService(testAppName, namespace)
	})
	It("should restrict tenant fields and preserve infra annotations on reconciliation", func() {
		ctx := context.Background()
		service.Name = "tenant-field-policy"
		service.Annotations = map[string]string{"example.com/tenant": "not-propagated"}
		service.Spec.LoadBalancerIP = "203.0.113.10"
		service.Spec.LoadBalancerSourceRanges = []string{"203.0.113.0/24"}
		resource.Create(tenantClient, service)
		DeferCleanup(func() { resource.Delete(tenantClient, service) })

		var infra *v1.Service
		Eventually(func() error {
			var err error
			infra, err = findInfraLoadBalancerService(service.Name, service.Namespace)
			return err
		}, time.Minute, time.Second).Should(Succeed())
		// The kubevirtci Deployment mounts this secret as its tenant kubeconfig
		// and uses shared credentials. Check that identity, not the test client's
		// credentials, before asserting event delivery.
		By("checking event authorization with the deployed CCM's tenant credentials")
		credentials := &v1.Secret{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Namespace: infra.Namespace, Name: "kubeconfig"}, credentials)).To(Succeed())
		config, err := clientcmd.RESTConfigFromKubeConfig(credentials.Data["kubeconfig"])
		Expect(err).NotTo(HaveOccurred())
		// run-e2e.sh forwards the tenant API to localhost for the external test
		// runner. The Secret's Service IP is only reachable inside infra.
		// Change the route, retaining the CCM identity and original TLS trust.
		runnerConfig, err := clientcmd.BuildConfigFromFlags("", tenantKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		originalEndpoint, err := url.Parse(config.Host)
		Expect(err).NotTo(HaveOccurred())
		if config.ServerName == "" {
			config.ServerName = originalEndpoint.Hostname()
		}
		config.Host = runnerConfig.Host
		config.Timeout = 15 * time.Second
		authClient, err := authorizationclient.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())
		for _, verb := range []string{"create", "patch"} {
			access, err := authClient.SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb: verb, Group: "", Resource: "events", Namespace: service.Namespace,
				}},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(access.Status.Allowed).To(BeTrue(), "CCM tenant credentials must permit %s events: %s", verb, access.Status.Reason)
		}
		Expect(infra.Annotations).NotTo(HaveKey("example.com/tenant"))
		Expect(infra.Spec.LoadBalancerIP).To(BeEmpty())
		Expect(infra.Spec.LoadBalancerSourceRanges).To(Equal([]string{"203.0.113.0/24"}))
		Eventually(func() bool {
			events := &v1.EventList{}
			if err := tenantClient.List(ctx, events, client.InNamespace(service.Namespace)); err != nil {
				return false
			}
			for _, event := range events.Items {
				if event.InvolvedObject.UID == service.UID && event.Reason == "LoadBalancerIPIgnored" && event.Type == v1.EventTypeWarning {
					return true
				}
			}
			return false
		}, time.Minute, time.Second).Should(BeTrue())

		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := infraClient.Get(ctx, naming.NamespacedName(infra), infra); err != nil {
				return err
			}
			if infra.Annotations == nil {
				infra.Annotations = map[string]string{}
			}
			infra.Annotations["example.com/infra"] = "retain"
			return infraClient.Update(ctx, infra)
		})).To(Succeed())
		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := tenantClient.Get(ctx, naming.NamespacedName(service), service); err != nil {
				return err
			}
			service.Spec.LoadBalancerSourceRanges = []string{"198.51.100.0/24"}
			return tenantClient.Update(ctx, service)
		})).To(Succeed())
		Eventually(func() []string {
			if err := infraClient.Get(ctx, naming.NamespacedName(infra), infra); err != nil {
				return nil
			}
			return infra.Spec.LoadBalancerSourceRanges
		}, time.Minute, time.Second).Should(Equal([]string{"198.51.100.0/24"}))
		Expect(infra.Annotations).To(HaveKeyWithValue("example.com/infra", "retain"))
	})
	Context("when a LB service is created in tenant cluster", func() {
		BeforeEach(func() {
			resource.Create(tenantClient, server)
			Eventually(func() bool {
				err = tenantClient.Get(context.TODO(), naming.NamespacedName(server), server)
				Expect(err).NotTo(HaveOccurred())
				return server.Status.ReadyReplicas == *server.Spec.Replicas
			}, 60*time.Second, time.Second).Should(BeTrue())
			resource.Create(tenantClient, service)
			DeferCleanup(func() {
				resource.Delete(tenantClient, server)
				resource.Delete(tenantClient, service)
			})
		})

		It("should succeed to curl the LB service", func() {
			loadBalancerService, err := findInfraLoadBalancerService(service.Name, service.Namespace)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() []v1.LoadBalancerIngress {
				err = infraClient.Get(context.TODO(), naming.NamespacedName(loadBalancerService), loadBalancerService)
				Expect(err).NotTo(HaveOccurred())
				return loadBalancerService.Status.LoadBalancer.Ingress
			}, 10*time.Second, time.Second).Should(HaveLen(1))

			curlJob = resources.CurlLoadBalancerJob("curl-test", tenantClusterName, loadBalancerService.Status.LoadBalancer.Ingress[0].IP, strconv.FormatInt(int64(loadBalancerService.Spec.Ports[0].Port), 10))
			err = infraClient.Create(context.TODO(), curlJob)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				resource.Delete(infraClient, curlJob, &client.DeleteOptions{PropagationPolicy: &backgroundPropagation})
			})

			Eventually(func() int {
				err := infraClient.Get(context.TODO(), naming.NamespacedName(curlJob), curlJob)
				Expect(err).NotTo(HaveOccurred())
				return int(curlJob.Status.Succeeded)
			}, time.Second*30, time.Second).Should(BeNumerically(">", 0))
		})
	})
})

func findInfraLoadBalancerService(tenantServiceName string, tenantServiceNamespace string) (*v1.Service, error) {
	lbService := v1.Service{}
	retryInterval := wait.Backoff{
		Steps:    5,
		Duration: 250 * time.Millisecond,
		Factor:   2,
		Jitter:   0.1,
	}
	lbServiceNotFoundError := fmt.Errorf("infra LoadBalancer service not found")
	isRetriable := func(err error) bool {
		return err == lbServiceNotFoundError
	}
	err := retry.OnError(retryInterval, isRetriable, func() error {
		serviceList := v1.ServiceList{}
		err := infraClient.List(context.TODO(), &serviceList)
		if err != nil {
			return err
		}
		for _, s := range serviceList.Items {
			lbService = s
			if isLoadBalancerServiceType(lbService) && hasLabel(lbService.Labels, "tenant-service-name", tenantServiceName) && hasLabel(lbService.Labels, "tenant-service-namespace", tenantServiceNamespace) {
				return nil
			}
		}
		return lbServiceNotFoundError
	})
	if err != nil {
		return nil, err
	}

	return &lbService, nil
}

func isLoadBalancerServiceType(service v1.Service) bool {
	return service.Spec.Type == v1.ServiceTypeLoadBalancer
}

func hasLabel(labels map[string]string, key string, value string) bool {
	for k, v := range labels {
		if k == fmt.Sprintf("cluster.x-k8s.io/%s", key) && v == value {
			return true
		}
	}
	return false
}
