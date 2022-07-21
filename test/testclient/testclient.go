package testclient

import (
	"os"

	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateClientForKubeconfig(kubeconfig string) (client.Client, error) {
	kc, err := os.ReadFile(kubeconfig)
	if err != nil {
		return nil, err
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kc)
	if err != nil {
		return nil, err
	}

	client, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}
	return client, nil
}
