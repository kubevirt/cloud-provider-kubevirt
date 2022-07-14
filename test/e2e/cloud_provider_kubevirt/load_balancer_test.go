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

	"kubevirt.io/cloud-provider-kubevirt/test/resources"
)

const namespace = "default"
const deploymentName = "test-server"

var _ = Describe("Load Balancer", func() {
	var (
		err                     error
		server                  *appsv1.Deployment
		service                 *v1.Service
		curlJob                 *batchv1.Job
		orphanPropagationpolicy = metav1.DeletePropagationBackground
	)

	Context("when a LB service is created in tenant cluster", func() {
		BeforeEach(func() {
			server, err = tenantClient.AppsV1().Deployments(namespace).Create(context.TODO(), resources.HTTPServerDeployment(deploymentName), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				server, err = tenantClient.AppsV1().Deployments(namespace).Get(context.TODO(), server.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				return server.Status.ReadyReplicas == *server.Spec.Replicas
			}, 60*time.Second, time.Second).Should(BeTrue())

			service, err = tenantClient.CoreV1().Services(namespace).Create(context.TODO(), resources.HTTPServerService(deploymentName), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				err := tenantClient.AppsV1().Deployments(namespace).Delete(context.TODO(), server.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() error {
					_, err = tenantClient.AppsV1().Deployments(namespace).Get(context.TODO(), server.Name, metav1.GetOptions{})
					return err
				}, 120*time.Second, time.Second).Should(WithTransform(errors.IsNotFound, BeTrue()))

				err = tenantClient.CoreV1().Services(namespace).Delete(context.TODO(), service.Name, metav1.DeleteOptions{})
				Expect(err).NotTo(HaveOccurred())
				Eventually(func() error {
					_, err := tenantClient.CoreV1().Services(namespace).Get(context.TODO(), service.Name, metav1.GetOptions{})
					return err
				}, 120*time.Second, time.Second).Should(WithTransform(errors.IsNotFound, BeTrue()))
			})
		})

		It("should succeed to curl the LB service", func() {
			loadBalancerService, err := findInfraLoadBalancerService(tenantClusterName, service.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(loadBalancerService.Status.LoadBalancer.Ingress).To(HaveLen(1))
			Expect(loadBalancerService.Spec.Ports).To(HaveLen(1))

			curlJob = resources.CurlLoadBalancerJob("curl-test", loadBalancerService.Status.LoadBalancer.Ingress[0].IP, strconv.FormatInt(int64(loadBalancerService.Spec.Ports[0].Port), 10))
			curlJob, err = client.BatchV1().Jobs(tenantClusterName).Create(
				context.TODO(),
				curlJob,
				metav1.CreateOptions{},
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				err := client.BatchV1().Jobs(tenantClusterName).Delete(context.TODO(), curlJob.Name, metav1.DeleteOptions{PropagationPolicy: &orphanPropagationpolicy})
				Expect(err).NotTo(HaveOccurred())
			})

			Eventually(func() int {
				job, err := client.BatchV1().Jobs(tenantClusterName).Get(context.TODO(), curlJob.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				return int(job.Status.Succeeded)
			}, time.Second*30, time.Second).Should(BeNumerically(">", 0))
		})
	})
})

func findInfraLoadBalancerService(tenantNamespace, tenantServiceName string) (*v1.Service, error) {
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
		services, err := client.CoreV1().Services(tenantNamespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, s := range services.Items {
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
