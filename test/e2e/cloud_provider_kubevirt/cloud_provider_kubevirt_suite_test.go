package cloud_provider_kubevirt

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/cloud-provider-kubevirt/test/environment"
	"kubevirt.io/cloud-provider-kubevirt/test/testclient"
)

var (
	tenantClusterName = environment.GetVarWithDefault("TENANT_CLUSTER_NAME", "kvcluster")

	kubeconfig       = environment.GetVarWithDefault("INFRA_KUBECONFIG", "config/kubevirtci/infra-kubeconfig")
	tenantKubeconfig = environment.GetVarWithDefault("TENANT_KUBECONFIG", "config/kubevirtci/kubeconfig")

	infraClient  client.Client
	tenantClient client.Client
)

func TestCloudProviderKubevirt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CloudProviderKubevirt Suite")
}

var _ = BeforeSuite(func() {
	var err error
	infraClient, err = testclient.CreateClientForKubeconfig(kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	tenantClient, err = testclient.CreateClientForKubeconfig(tenantKubeconfig)
	Expect(err).NotTo(HaveOccurred())
})
