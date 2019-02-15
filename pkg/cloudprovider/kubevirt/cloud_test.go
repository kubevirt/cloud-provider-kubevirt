package kubevirt

import (
	"testing"
)

func TestKubevirtCloudProviderFactory(t *testing.T) {
	// Calling kubevirtCloudProviderFactory without config should return an error
	_, err := kubevirtCloudProviderFactory(nil)
	if err.Error() != "No kubevirt cloud provider config file given" {
		t.Error("Kubevirt cloudprovider Factory should return an error if invoked with nil.")
	}

	// TODO: Test factory with defined cloud-configs
}
