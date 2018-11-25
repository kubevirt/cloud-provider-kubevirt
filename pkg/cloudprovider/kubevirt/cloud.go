package kubevirt

import (
	"bytes"
	"fmt"
	"io"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/controller"
	"kubevirt.io/kubevirt/pkg/kubecli"
)

const (
	providerName = "kubevirt"
)

type cloud struct {
	config     clientcmd.ClientConfig
	kubernetes kubernetes.Clientset
	kubevirt   kubecli.KubevirtClient
}

func init() {
	cloudprovider.RegisterCloudProvider(providerName, func(config io.Reader) (cloudprovider.Interface, error) {
		if config == nil {
			return nil, fmt.Errorf("No %s cloud provider config file given", providerName)
		}

		buf := new(bytes.Buffer)
		buf.ReadFrom(config)
		// TODO(dgonzalez): Replace by clientcmd.NewClientConfigFromBytes when updating client-go
		apiConfig, err := clientcmd.Load(buf.Bytes())
		if err != nil {
			return nil, err
		}
		clientConfig := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{})
		kubernetesClient, kubevirtClient, err := createClients(clientConfig)
		if err != nil {
			return nil, err
		}
		return &cloud{
			config:     clientConfig,
			kubernetes: *kubernetesClient,
			kubevirt:   *kubevirtClient,
		}, nil
	})
}

func createClients(clientConfig clientcmd.ClientConfig) (*kubernetes.Clientset, *kubecli.KubevirtClient, error) {
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		glog.Errorf("Failed to build REST config: %v", err)
		return nil, nil, err
	}
	kubernetesClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		glog.Errorf("Failed to create Kubernetes client: %v", err)
		return nil, nil, err
	}
	kubevirtClient, err := kubecli.GetKubevirtClientFromRESTConfig(restConfig)
	if err != nil {
		glog.Errorf("Failed to create KubeVirt client: %v", err)
		return nil, nil, err
	}
	return kubernetesClient, &kubevirtClient, nil
}

// Initialize provides the cloud with a kubernetes client builder and may spawn goroutines
// to perform housekeeping activities within the cloud provider.
func (c *cloud) Initialize(clientBuilder controller.ControllerClientBuilder) {}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	namespace, _, err := c.config.Namespace()
	if err != nil {
		glog.Errorf("Could not find namespace for loadbalancer: %v", err)
		return nil, false
	}
	return &loadbalancer{
		cloud:     c,
		namespace: namespace,
	}, true
}

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) Instances() (cloudprovider.Instances, bool) {
	namespace, _, err := c.config.Namespace()
	if err != nil {
		glog.Errorf("Could not find namespace for instances: %v", err)
		return nil, false
	}
	return &instances{
		cloud:     c,
		namespace: namespace,
	}, true
}

// Zones returns a zones interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) Zones() (cloudprovider.Zones, bool) {
	namespace, _, err := c.config.Namespace()
	if err != nil {
		glog.Errorf("Could not find namespace for zones: %v", err)
		return nil, false
	}
	return &zones{
		cloud:     c,
		namespace: namespace,
	}, true
}

// Clusters returns a clusters interface.  Also returns true if the interface is supported, false otherwise.
func (c *cloud) Clusters() (cloudprovider.Clusters, bool) {
	return nil, false
}

// Routes returns a routes interface along with whether the interface is supported.
func (c *cloud) Routes() (cloudprovider.Routes, bool) {
	return nil, false
}

// ProviderName returns the cloud provider ID.
func (c *cloud) ProviderName() string {
	return providerName
}

// HasClusterID returns true if a ClusterID is required and set
func (c *cloud) HasClusterID() bool {
	return true
}
