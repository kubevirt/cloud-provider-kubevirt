package kubevirt

// import (
// 	"fmt"
// 	"reflect"
// 	"strings"
// 	"testing"
// )

// const kubeconfig = `apiVersion: v1
// clusters:
// - cluster:
//     certificate-authority-data: cert-auth-data
//     server: https://127.0.0.1:6443
//   name: kubernetes
// contexts:
// - context:
//     cluster: kubernetes
//     user: kubernetes-admin
//     namespace: default
//   name: kubernetes-admin@kubernetes
// current-context: kubernetes-admin@kubernetes
// kind: Config
// preferences: {}
// users:
// - name: kubernetes-admin
//   user:
//     client-certificate-data: cert-data
//     client-key-data: key-data
// `

// var (
// 	minimalConf      = fmt.Sprintf("kubeconfig: |\n%s", indent(kubeconfig, "  "))
// 	loadbalancerConf = fmt.Sprintf("kubeconfig: |\n%s\nloadbalancer:\n  enabled: %t\n  creationPollInterval: %d", indent(kubeconfig, "  "), false, 3)
// 	instancesConf    = fmt.Sprintf("kubeconfig: |\n%s\ninstances:\n  enabled: %t\n  enableInstanceTypes: %t", indent(kubeconfig, "  "), false, true)
// 	zonesConf        = fmt.Sprintf("kubeconfig: |\n%s\nzones:\n  enabled: %t", indent(kubeconfig, "  "), false)
// 	allConf          = fmt.Sprintf("kubeconfig: |\n%s\nloadbalancer:\n  enabled: %t\ninstances:\n  enabled: %t\nzones:\n  enabled: %t", indent(kubeconfig, "  "), false, false, false)
// 	invalidKubeconf  = "kubeconfig: bla"
// )

// func indent(s, indent string) string {
// 	return indent + strings.ReplaceAll(s, "\n", fmt.Sprintf("\n%s", indent))
// }

// func makeCloudConfig(kubeconfig string, loadbalancerEnabled, instancesEnabled, zonesEnabled bool, lbCreationPollInterval int, enableInstanceTypes bool) CloudConfig {
// 	return CloudConfig{
// 		Kubeconfig: kubeconfig,
// 		LoadBalancer: LoadBalancerConfig{
// 			Enabled:              loadbalancerEnabled,
// 			CreationPollInterval: lbCreationPollInterval,
// 		},
// 		Instances: InstancesConfig{
// 			Enabled:             instancesEnabled,
// 			EnableInstanceTypes: enableInstanceTypes,
// 		},
// 		Zones: ZonesConfig{
// 			Enabled: zonesEnabled,
// 		},
// 	}
// }

// func TestNewCloudConfigFromBytes(t *testing.T) {
// 	tests := []struct {
// 		configBytes         string
// 		expectedCloudConfig CloudConfig
// 		expectedError       error
// 	}{
// 		{minimalConf, makeCloudConfig(kubeconfig, true, true, true, 5, false), nil},
// 		{loadbalancerConf, makeCloudConfig(kubeconfig, false, true, true, 3, false), nil},
// 		{instancesConf, makeCloudConfig(kubeconfig, true, false, true, 5, true), nil},
// 		{zonesConf, makeCloudConfig(kubeconfig, true, true, false, 5, false), nil},
// 		{allConf, makeCloudConfig(kubeconfig, false, false, false, 5, false), nil},
// 	}

// 	for _, test := range tests {
// 		config, err := NewCloudConfigFromBytes([]byte(test.configBytes))
// 		if !reflect.DeepEqual(config, test.expectedCloudConfig) {
// 			t.Errorf("Expected: %v, got %v", test.expectedCloudConfig, config)
// 		}
// 		if test.expectedError != nil && err != nil && err.Error() != test.expectedError.Error() {
// 			t.Errorf("Expected: '%v', got '%v'", test.expectedError, err)
// 		}
// 	}
// }

// func TestKubevirtCloudProviderFactory(t *testing.T) {
// 	// Calling kubevirtCloudProviderFactory without config should return an error
// 	_, err := kubevirtCloudProviderFactory(nil)
// 	if err == nil {
// 		t.Error("Expected: 'No kubevirt cloud provider config file given', got 'nil'")
// 	} else if err.Error() != "No kubevirt cloud provider config file given" {
// 		t.Errorf("Expected: 'No kubevirt cloud provider config file given', got '%v'", err)
// 	}

// 	_, err = kubevirtCloudProviderFactory(strings.NewReader(invalidKubeconf))
// 	if err == nil || !strings.Contains(err.Error(), "couldn't get version/kind; json parse error") {
// 		t.Errorf("Expected error containing: 'couldn't get version/kind; json parse error', got '%v'", err)
// 	}
// }
