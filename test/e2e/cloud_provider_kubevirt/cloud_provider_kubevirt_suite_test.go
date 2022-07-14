package cloud_provider_kubevirt

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"

	"kubevirt.io/cloud-provider-kubevirt/test/environment"
	"kubevirt.io/cloud-provider-kubevirt/test/testclient"
)

var (
	tenantClusterName = environment.GetVarWithDefault("TENANT_CLUSTER_NAME", "kvcluster")

	kubeconfig       = environment.GetVarWithDefault("INFRA_KUBECONFIG", "config/secret/infra-kubeconfig")
	tenantKubeconfig = environment.GetVarWithDefault("TENANT_KUBECONFIG", "config/secret/kubeconfig")

	client       *kubernetes.Clientset
	tenantClient *kubernetes.Clientset
)

func TestCloudProviderKubevirt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CloudProviderKubevirt Suite")
}

var _ = BeforeSuite(func() {
	var err error
	client, err = testclient.CreateClientForKubeconfig(kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	tenantClient, err = testclient.CreateClientForKubeconfig(tenantKubeconfig)
	Expect(err).NotTo(HaveOccurred())
})
