package provider

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ProviderName is the name of the kubevirt provider
	ProviderName = "kubevirt"
)

var scheme = runtime.NewScheme()

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, kubevirtCloudProviderFactory)
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := kubevirtv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

type cloud struct {
	namespace string
	client    client.Client
	config    CloudConfig
}

type CloudConfig struct {
	Kubeconfig   string             `yaml:"kubeconfig"`
	LoadBalancer LoadBalancerConfig `yaml:"loadBalancer"`
	InstancesV2  InstancesV2Config  `yaml:"instancesV2"`
	Namespace    string             `yaml:"namespace"`
	InfraLabels  map[string]string  `yaml:"infraLabels"`
}

type LoadBalancerConfig struct {
	// Enabled activates the load balancer interface of the CCM
	Enabled bool `yaml:"enabled"`

	// CreationPollInterval determines how many seconds to wait for the load balancer creation between retries
	CreationPollInterval *int `yaml:"creationPollInterval,omitempty"`

	// CreationPollTimeout determines how many seconds to wait for the load balancer creation
	CreationPollTimeout *int `yaml:"creationPollTimeout,omitempty"`
}

type InstancesV2Config struct {
	// Enabled activates the instances interface of the CCM
	Enabled bool `yaml:"enabled"`
	// ZoneAndRegionEnabled indicates if need to get Region and zone labels from the cloud provider
	ZoneAndRegionEnabled bool `yaml:"zoneAndRegionEnabled"`
}

// createDefaultCloudConfig creates a CloudConfig object filled with default values.
// These default values should be overwritten by values read from the cloud-config file.
func createDefaultCloudConfig() CloudConfig {
	return CloudConfig{
		LoadBalancer: LoadBalancerConfig{
			Enabled:              true,
			CreationPollInterval: pointer.Int(int(defaultLoadBalancerCreatePollInterval.Seconds())),
			CreationPollTimeout:  pointer.Int(int(defaultLoadBalancerCreatePollTimeout.Seconds())),
		},
		InstancesV2: InstancesV2Config{
			Enabled:              true,
			ZoneAndRegionEnabled: true,
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
	namespace := cloudConf.Namespace
	var restConfig *rest.Config
	if cloudConf.Kubeconfig == "" {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		var infraKubeConfig string
		infraKubeConfig, err = getInfraKubeConfig(cloudConf.Kubeconfig)
		if err != nil {
			return nil, err
		}
		var clientConfig clientcmd.ClientConfig
		clientConfig, err = clientcmd.NewClientConfigFromBytes([]byte(infraKubeConfig))
		if err != nil {
			return nil, err
		}
		restConfig, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, err
		}
		if namespace == "" {
			namespace, _, err = clientConfig.Namespace()
			if err != nil {
				klog.Errorf("Could not find namespace in client config: %v", err)
				return nil, err
			}
		}
	}
	c, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
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
		namespace:   c.namespace,
		client:      c.client,
		config:      c.config.LoadBalancer,
		infraLabels: c.config.InfraLabels,
	}, true
}

// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
func (c *cloud) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

func (c *cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	if !c.config.InstancesV2.Enabled {
		return nil, false
	}
	return &instancesV2{
		namespace: c.namespace,
		client:    c.client,
		config:    &c.config.InstancesV2,
	}, true
}

// Zones returns a zones interface. Also returns true if the interface is supported, false otherwise.
// DEPRECATED: Zones is deprecated in favor of retrieving zone/region information from InstancesV2.
func (c *cloud) Zones() (cloudprovider.Zones, bool) {
	return nil, false
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

func getInfraKubeConfig(infraKubeConfigPath string) (string, error) {
	config, err := os.Open(infraKubeConfigPath)
	if err != nil {
		return "", fmt.Errorf("Couldn't open infra-kubeconfig: %v", err)
	}
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(config)
	if err != nil {
		return "", fmt.Errorf("Failed to read infra-kubeconfig: %v", err)
	}
	return buf.String(), nil
}
