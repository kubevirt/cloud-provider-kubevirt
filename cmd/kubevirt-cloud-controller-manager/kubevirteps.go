package main

import (
	"context"
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider/app"
	"k8s.io/cloud-provider/app/config"
	genericcontrollermanager "k8s.io/controller-manager/app"
	"k8s.io/controller-manager/controller"
	"k8s.io/klog/v2"
	"kubevirt.io/cloud-provider-kubevirt/pkg/controller/kubevirteps"
	kubevirt "kubevirt.io/cloud-provider-kubevirt/pkg/provider"
)

func StartKubevirtCloudControllerWrapper(initContext app.ControllerInitContext, completedConfig *config.CompletedConfig, cloud cloudprovider.Interface) app.InitFunc {
	return func(ctx context.Context, controllerContext genericcontrollermanager.ControllerContext) (controller.Interface, bool, error) {
		return startKubevirtCloudController(ctx, initContext, controllerContext, completedConfig, cloud)
	}
}

func startKubevirtCloudController(
	ctx context.Context,
	initContext app.ControllerInitContext,
	controllerContext genericcontrollermanager.ControllerContext,
	ccmConfig *config.CompletedConfig,
	cloud cloudprovider.Interface) (controller.Interface, bool, error) {

	klog.Infof(fmt.Sprintf("Starting %s.", kubevirteps.ControllerName))

	kubevirtCloud, ok := cloud.(*kubevirt.Cloud)
	if !ok {
		err := fmt.Errorf("%s does not support %v provider", kubevirteps.ControllerName, cloud.ProviderName())
		return nil, false, err
	}

	if kubevirtCloud.GetCloudConfig().LoadBalancer.EnableEPSController == nil || !*kubevirtCloud.GetCloudConfig().LoadBalancer.EnableEPSController {
		klog.Infof(fmt.Sprintf("%s is not enabled.", kubevirteps.ControllerName))
		return nil, false, nil
	}

	klog.Infof("Setting up tenant client.")

	var tenantClient kubernetes.Interface
	// This is the kubeconfig for the tenant (in-cluster) cluster
	tenantClient, err := kubernetes.NewForConfig(ccmConfig.Complete().Kubeconfig)
	if err != nil {
		return nil, false, err
	}

	klog.Infof("Setting up infra client.")

	// This is the kubeconfig for the infra cluster
	var restConfig *rest.Config

	if kubevirtCloud.GetCloudConfig().Kubeconfig == "" {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			klog.Errorf("Failed to get in-cluster config: %v", err)
			return nil, false, err
		}
	} else {
		var infraKubeConfig string
		infraKubeConfig, err = kubevirtCloud.GetInfraKubeconfig()
		if err != nil {
			klog.Errorf("Failed to get infra kubeconfig: %v", err)
			return nil, false, err
		}
		var clientConfig clientcmd.ClientConfig
		clientConfig, err = clientcmd.NewClientConfigFromBytes([]byte(infraKubeConfig))
		if err != nil {
			klog.Errorf("Failed to create client config from infra kubeconfig: %v", err)
			return nil, false, err
		}
		restConfig, err = clientConfig.ClientConfig()
		if err != nil {
			klog.Errorf("Failed to create rest config for infra cluster: %v", err)
			return nil, false, err
		}
	}

	var infraClient kubernetes.Interface

	// create new client for the infra cluster
	infraClient, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create infra cluster client: %v", err)
		return nil, false, err
	}

	var infraDynamic dynamic.Interface

	infraDynamic, err = dynamic.NewForConfig(restConfig)
	if err != nil {
		klog.Errorf("Failed to create dynamic client for infra cluster: %v", err)
		return nil, false, err
	}

	klog.Infof("Setting up kubevirtEPSController")

	kubevirtEPSController := kubevirteps.NewKubevirtEPSController(tenantClient, infraClient, infraDynamic, kubevirtCloud.Namespace())

	klog.Infof("Initializing kubevirtEPSController")

	err = kubevirtEPSController.Init()
	if err != nil {
		klog.Errorf("Failed to initialize kubevirtEPSController: %v", err)
		return nil, false, err
	}

	klog.Infof("Running kubevirtEPSController")
	go kubevirtEPSController.Run(1, controllerContext.Stop, controllerContext.ControllerManagerMetrics)

	return nil, false, nil
}
