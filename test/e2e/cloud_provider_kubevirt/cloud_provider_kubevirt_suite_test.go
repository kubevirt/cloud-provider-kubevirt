package cloud_provider_kubevirt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/cloud-provider-kubevirt/test/environment"
	"kubevirt.io/cloud-provider-kubevirt/test/testclient"
)

var (
	tenantClusterName = environment.GetVarWithDefault("TENANT_CLUSTER_NAME", "kvcluster")

	kubeconfig       = environment.GetVarWithDefault("INFRA_KUBECONFIG", "config/secret/infra-kubeconfig")
	tenantKubeconfig = environment.GetVarWithDefault("TENANT_KUBECONFIG", "config/secret/kubeconfig")
	dumpPath         = os.Getenv("DUMP_PATH")

	infraClient  client.Client
	tenantClient client.Client
)

func TestCloudProviderKubevirt(t *testing.T) {
	RegisterFailHandler(Fail)

	if dumpPath != "" {
		if _, err := os.Stat(dumpPath); os.IsNotExist(err) {
			t.Fatalf("invalid dump-path: %s doesn't exist", dumpPath)
		}
	}

	RunSpecs(t, "CloudProviderKubevirt Suite")
}

var _ = BeforeSuite(func() {
	var err error
	infraClient, err = testclient.CreateClientForKubeconfig(kubeconfig)
	Expect(err).NotTo(HaveOccurred())

	tenantClient, err = testclient.CreateClientForKubeconfig(tenantKubeconfig)
	Expect(err).NotTo(HaveOccurred())
})

var _ = JustAfterEach(func() {
	if CurrentSpecReport().Failed() && dumpPath != "" {
		dump(os.Getenv("KUBECONFIG"), "")
	}
})

func dump(kubeconfig, artifactsSuffix string) {
	cmd := exec.Command(dumpPath, "--kubeconfig", kubeconfig)

	failureLocation := CurrentSpecReport().Failure.Location
	artifactsPath := filepath.Join(os.Getenv("ARTIFACTS"), fmt.Sprintf("%s:%d", filepath.Base(failureLocation.FileName), failureLocation.LineNumber), artifactsSuffix)
	cmd.Env = append(cmd.Env, fmt.Sprintf("ARTIFACTS=%s", artifactsPath))

	By(fmt.Sprintf("dumping k8s artifacts to %s", artifactsPath))
	output, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(output))
}
