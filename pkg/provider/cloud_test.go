package provider

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const kubeconfig = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: cert-auth-data
    server: https://127.0.0.1:6443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
    namespace: default
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: cert-data
    client-key-data: key-data
`

var (
	ns                = "aNamespace"
	minimalConf       = fmt.Sprintf("kubeconfig: |\n%s", indent(kubeconfig, "  "))
	loadbalancerConf  = fmt.Sprintf("kubeconfig: |\n%s\nloadBalancer:\n  enabled: %t\n  creationPollInterval: %d", indent(kubeconfig, "  "), false, 3)
	instancesConf     = fmt.Sprintf("kubeconfig: |\n%s\ninstancesV2:\n  enabled: %t\n  enableInstanceTypes: %t", indent(kubeconfig, "  "), false, true)
	zoneAndRegionConf = fmt.Sprintf("kubeconfig: |\n%s\ninstancesV2:\n  zoneAndRegionEnabled: %t\n  enableInstanceTypes: %t", indent(kubeconfig, "  "), false, true)
	namespaceConf     = fmt.Sprintf("kubeconfig: |\n%s\nnamespace: %s", indent(kubeconfig, "  "), ns)
	allConf           = fmt.Sprintf("kubeconfig: |\n%s\nloadBalancer:\n  enabled: %t\ninstancesV2:\n  enabled: %t\nnamespace: %s", indent(kubeconfig, "  "), false, false, ns)
	invalidKubeconf   = "kubeconfig: bla"
)

func indent(s, indent string) string {
	return indent + strings.ReplaceAll(s, "\n", fmt.Sprintf("\n%s", indent))
}

func makeCloudConfig(kubeconfig, namespace string, loadbalancerEnabled, instancesEnabled bool, zoneAndRegionEnabled bool, lbCreationPollInterval int) CloudConfig {
	return CloudConfig{
		Kubeconfig: kubeconfig,
		LoadBalancer: LoadBalancerConfig{
			Enabled:              loadbalancerEnabled,
			CreationPollInterval: lbCreationPollInterval,
		},
		InstancesV2: InstancesV2Config{
			Enabled:              instancesEnabled,
			ZoneAndRegionEnabled: zoneAndRegionEnabled,
		},
		Namespace: namespace,
	}
}

func TestNewCloudConfigFromBytes(t *testing.T) {
	tests := []struct {
		configBytes         string
		expectedCloudConfig CloudConfig
		expectedError       error
	}{
		{minimalConf, makeCloudConfig(kubeconfig, "", true, true, true, 5), nil},
		{loadbalancerConf, makeCloudConfig(kubeconfig, "", false, true, true, 3), nil},
		{instancesConf, makeCloudConfig(kubeconfig, "", true, false, true, 5), nil},
		{zoneAndRegionConf, makeCloudConfig(kubeconfig, "", true, true, false, 5), nil},
		{namespaceConf, makeCloudConfig(kubeconfig, ns, true, true, true, 5), nil},
		{allConf, makeCloudConfig(kubeconfig, ns, false, false, true, 5), nil},
	}

	for _, test := range tests {
		config, err := NewCloudConfigFromBytes([]byte(test.configBytes))
		if !reflect.DeepEqual(config, test.expectedCloudConfig) {
			t.Errorf("Expected: %v, got %v", test.expectedCloudConfig, config)
		}
		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
		}
	}
}

func TestKubevirtCloudProviderFactory(t *testing.T) {
	// Calling kubevirtCloudProviderFactory without config should return an error
	_, err := kubevirtCloudProviderFactory(nil)
	if err == nil {
		t.Error("Expected: 'No kubevirt cloud provider config file given', got 'nil'")
	} else if err.Error() != "No kubevirt cloud provider config file given" {
		t.Errorf("Expected: 'No kubevirt cloud provider config file given', got '%v'", err)
	}

	_, err = kubevirtCloudProviderFactory(strings.NewReader(invalidKubeconf))
	if err == nil || !strings.Contains(err.Error(), "couldn't get version/kind; json parse error") {
		t.Errorf("Expected error containing: 'couldn't get version/kind; json parse error', got '%v'", err)
	}
}