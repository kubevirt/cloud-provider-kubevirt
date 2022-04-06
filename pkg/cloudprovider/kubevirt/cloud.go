package kubevirt

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v2"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ProviderName is the name of the kubevirt provider
	ProviderName = "kubevirt"
)

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, kubevirtCloudProviderFactory)
}

type cloud struct {
	namespace string
	client    client.Client
	config    CloudConfig
}

type CloudConfig struct {
	Kubeconfig   string             `yaml:"kubeconfig"` // The kubeconfig used to connect to the underkube
	LoadBalancer LoadBalancerConfig `yaml:"loadbalancer"`
	Instances    InstancesConfig    `yaml:"instances"`
	Zones        ZonesConfig        `yaml:"zones"`
}

type LoadBalancerConfig struct {
	Enabled              bool `yaml:"enabled"`              // Enables the loadbalancer interface of the CCM
	CreationPollInterval int  `yaml:"creationPollInterval"` // How many seconds to wait for the loadbalancer creation
}

type InstancesConfig struct {
	Enabled             bool `yaml:"enabled"`             // Enables the instances interface of the CCM
	EnableInstanceTypes bool `yaml:"enableInstanceTypes"` // Enables 'flavor' annotation to detect instance types
}

type ZonesConfig struct {
	Enabled bool `yaml:"enabled"` // Enables the zones interface of the CCM
}

// createDefaultCloudConfig creates a CloudConfig object filled with default values.
// These default values should be overwritten by values read from the cloud-config file.
func createDefaultCloudConfig() CloudConfig {
	return CloudConfig{
		LoadBalancer: LoadBalancerConfig{
			Enabled:              true,
			CreationPollInterval: defaultLoadBalancerCreatePollInterval,
		},
		Instances: InstancesConfig{
			Enabled: true,
		},
		Zones: ZonesConfig{
			Enabled: true,
		},
	}
}

func NewCloudConfigFromBytes(configBytes []byte) (CloudConfig, error) {
	var config = createDefaultCloudConfig()
	err := yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return CloudConfig{}, err
	}
	return config, nil
}

func kubevirtCloudProviderFactory(config io.Reader) (cloudprovider.Interface, error) {
	if config == nil {
		return nil, fmt.Errorf("No %s cloud provider config file given", ProviderName)
	}

	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(config)
	if err != nil {
		return nil, fmt.Errorf("Failed to read cloud provider config: %v", err)
	}
	cloudConf, err := NewCloudConfigFromBytes(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal cloud provider config: %v", err)
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes([]byte(cloudConf.Kubeconfig))
	if err != nil {
		return nil, err
	}
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		klog.Errorf("Could not find namespace in client config: %v", err)
		return nil, err
	}
	c, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, err
	}
	return &cloud{
		namespace: namespace,
		client:    c,
		config:    cloudConf,
	}, nil
}

// Initialize provides the cloud with a kubernetes client builder and may spawn goroutines
// to perform housekeeping activities within the cloud provider.
func (c *cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	if !c.config.LoadBalancer.Enabled {
		return nil, false
	}
	return &loadbalancer{
		namespace: c.namespace,
		client:    c.client,
		config:    c.config.LoadBalancer,
	}, true
}

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) Instances() (cloudprovider.Instances, bool) {
	if !c.config.Instances.Enabled {
		return nil, false
	}
	return &instances{
		namespace: c.namespace,
		client:    c.client,
		config:    c.config.Instances,
	}, true
}

func (c *cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return nil, false
}

// Zones returns a zones interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) Zones() (cloudprovider.Zones, bool) {
	if !c.config.Zones.Enabled {
		return nil, false
	}
	return &zones{
		namespace: c.namespace,
		client:    c.client,
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
	return ProviderName
}

// HasClusterID returns true if a ClusterID is required and set
func (c *cloud) HasClusterID() bool {
	return true
}
