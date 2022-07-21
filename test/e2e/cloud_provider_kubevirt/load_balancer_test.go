package cloud_provider_kubevirt

import (
	"context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	Context("when a LB service is created in tenant cluster", func() {
		BeforeEach(func() {
			err = tenantClient.Create(context.TODO(), server)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				err = tenantClient.Get(context.TODO(), namespacedName(server), server)
				Expect(err).NotTo(HaveOccurred())
				return server.Status.ReadyReplicas == *server.Spec.Replicas
			}, 60*time.Second, time.Second).Should(BeTrue())

			err = tenantClient.Create(context.TODO(), service)
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				err = tenantClient.Delete(context.TODO(), server)
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() error {
					return tenantClient.Get(context.TODO(), namespacedName(server), server)
				}, 120*time.Second, time.Second).Should(WithTransform(errors.IsNotFound, BeTrue()))

				err = tenantClient.Delete(context.TODO(), service)
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() error {
					return tenantClient.Get(context.TODO(), namespacedName(service), service)
				}, 120*time.Second, time.Second).Should(WithTransform(errors.IsNotFound, BeTrue()))
			})
		})

		It("should succeed to curl the LB service", func() {
			loadBalancerService, err := findInfraLoadBalancerService(service.Name)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() []v1.LoadBalancerIngress {
				err = infraClient.Get(context.TODO(), namespacedName(loadBalancerService), loadBalancerService)
				Expect(err).NotTo(HaveOccurred())
				return loadBalancerService.Status.LoadBalancer.Ingress
			}, 10*time.Second, time.Second).Should(HaveLen(1))

			curlJob = resources.CurlLoadBalancerJob("curl-test", tenantClusterName, loadBalancerService.Status.LoadBalancer.Ingress[0].IP, strconv.FormatInt(int64(loadBalancerService.Spec.Ports[0].Port), 10))
			err = infraClient.Create(context.TODO(), curlJob)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				err := infraClient.Delete(context.TODO(), curlJob, &client.DeleteOptions{PropagationPolicy: &backgroundPropagation})
				Expect(err).NotTo(HaveOccurred())
			})

			Eventually(func() int {
				err := infraClient.Get(context.TODO(), namespacedName(curlJob), curlJob)
				Expect(err).NotTo(HaveOccurred())
				return int(curlJob.Status.Succeeded)
			}, time.Second*30, time.Second).Should(BeNumerically(">", 0))
		})
	})
})

func findInfraLoadBalancerService(tenantServiceName string) (*v1.Service, error) {
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
			if isLoadBalancerServiceType(lbService) && hasSelector(lbService.Spec.Selector, lbService.Name, tenantServiceName) {
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

func hasSelector(selector map[string]string, key string, value string) bool {
	for k, v := range selector {
		if k == fmt.Sprintf("cloud.kubevirt.io/%s", key) && v == value {
			return true
		}
	}
	return false
}
