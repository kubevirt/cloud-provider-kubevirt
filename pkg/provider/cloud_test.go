package provider

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	ns                  = "aNamespace"
	infraKubeConfigPath = "infraKubeConfig"
	minimalConf         = fmt.Sprintf("kubeconfig: %s", infraKubeConfigPath)
	loadbalancerConf    = fmt.Sprintf("kubeconfig: %s\nloadBalancer:\n  enabled: %t\n  creationPollInterval: %d\n  creationPollTimeout: %d", infraKubeConfigPath, false, 3, 100)
	instancesConf       = fmt.Sprintf("kubeconfig: %s\ninstancesV2:\n  enabled: %t\n  enableInstanceTypes: %t", infraKubeConfigPath, false, true)
	zoneAndRegionConf   = fmt.Sprintf("kubeconfig: %s\ninstancesV2:\n  zoneAndRegionEnabled: %t\n  enableInstanceTypes: %t", infraKubeConfigPath, false, true)
	namespaceConf       = fmt.Sprintf("kubeconfig: %s\nnamespace: %s", infraKubeConfigPath, ns)
	allConf             = fmt.Sprintf("kubeconfig: %s\nloadBalancer:\n  enabled: %t\ninstancesV2:\n  enabled: %t\nnamespace: %s", infraKubeConfigPath, false, false, ns)
	invalidKubeconf     = "bla"
)

func makeCloudConfig(kubeconfig, namespace string, loadbalancerEnabled, instancesEnabled bool, zoneAndRegionEnabled bool, lbCreationPollInterval int, lbCreationPollTimeout int) CloudConfig {
	return CloudConfig{
		Kubeconfig: kubeconfig,
		LoadBalancer: LoadBalancerConfig{
			Enabled:              loadbalancerEnabled,
			CreationPollInterval: &lbCreationPollInterval,
			CreationPollTimeout:  &lbCreationPollTimeout,
		},
		InstancesV2: InstancesV2Config{
			Enabled:              instancesEnabled,
			ZoneAndRegionEnabled: zoneAndRegionEnabled,
		},
		Namespace: namespace,
	}
}

var _ = Describe("Cloud config", func() {

	DescribeTable("Get CloudConfig from bytes", func(configBytes string, expectedCloudConfig CloudConfig, expectedError error) {
		config, err := NewCloudConfigFromBytes([]byte(configBytes))
		Expect(config).To(Equal(expectedCloudConfig))
		Expect(err).To(BeNil())
	},
		Entry("With minimal configuration", minimalConf, makeCloudConfig(infraKubeConfigPath, "", true, true, true, 5, 300), nil),
		Entry("With loadBalancer configuration", loadbalancerConf, makeCloudConfig(infraKubeConfigPath, "", false, true, true, 3, 100), nil),
		Entry("With instance configuration", instancesConf, makeCloudConfig(infraKubeConfigPath, "", true, false, true, 5, 300), nil),
		Entry("With zone and region configuration", zoneAndRegionConf, makeCloudConfig(infraKubeConfigPath, "", true, true, false, 5, 300), nil),
		Entry("With namespace configuration", namespaceConf, makeCloudConfig(infraKubeConfigPath, ns, true, true, true, 5, 300), nil),
		Entry("With full configuration", allConf, makeCloudConfig(infraKubeConfigPath, ns, false, false, true, 5, 300), nil),
	)

	Describe("KubeVirt Cloud Factory", func() {

		Context("With nil config", func() {

			It("Should return an error", func() {
				_, err := kubevirtCloudProviderFactory(nil)
				Expect(err).To(HaveOccurred())
			})

		})

		Context("With invalid infraKubeConfig", func() {
			var (
				err             error
				infraKubeConfig *os.File
			)

			BeforeEach(func() {
				infraKubeConfig, err = ioutil.TempFile("", "infraKubeConfig")
				Expect(err).NotTo(HaveOccurred())
				_, err = infraKubeConfig.Write([]byte(invalidKubeconf))
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				os.Remove(infraKubeConfig.Name())
			})

			It("Should return an error", func() {
				_, err = kubevirtCloudProviderFactory(strings.NewReader(fmt.Sprintf("kubeconfig: %s", infraKubeConfig.Name())))
				Expect(err).To(HaveOccurred())
			})

		})

	})
})
