# cloud-provider-kubevirt
[![Build Status](https://travis-ci.org/kubevirt/cloud-provider-kubevirt.svg?branch=master)](https://travis-ci.org/kubevirt/cloud-provider-kubevirt)
[![Go Report Card](https://goreportcard.com/badge/github.com/kubevirt/cloud-provider-kubevirt)](https://goreportcard.com/report/github.com/kubevirt/cloud-provider-kubevirt)

The KubeVirt cloud-provider allows you to use KubeVirt and Kubernetes as a "cloud" to run Kubernetes clusters on top.
This project contains the `kubevirt-cloud-controller-manager`, an implementation of the cloud controller manager (see
[Concepts Underlying the Cloud Controller Manager](https://kubernetes.io/docs/concepts/architecture/cloud-controller/)
for more details).

## Introduction
The KubeVirt cloud-provider allows a Kubernetes cluster running in KubeVirt VMs (tenant cluster) to
interact with KubeVirt and Kubernetes (infrastructure cluster) to provision, manage and clean up resources. For example, the
cloud-provider ensures that [zone and region
labels](https://kubernetes.io/docs/reference/kubernetes-api/labels-annotations-taints/#failure-domainbetakubernetesiozone)
of nodes in the tenant cluster are set based on the zone and region of the KubeVirt VMs in the infrastructure cluster.
The cloud-provider also ensures tenant cluster services of type
[LoadBalancer](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) are properly exposed
through services in the UnderKube.

## Prerequisites
In order to have the LoadBalancer logic working in the "tenant KubeVirt cluster, user must make sure the the KubeVirt
VMs, used for the tenant cluster nodes, are created with the following labels:
```shell
cluster.x-k8s.io/cluster-name: <tenant-cluster-name>
cluster.x-k8s.io/role: worker
```
Those labels are used by the infra cluster services as a NodeSelector - traffic from the
infra cluster services created for the tenant cluster is redirected into VM with those Labels

## How to run `kubevirt-cloud-controller-manager`
See [Running cloud-controller-manager](https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/#running-cloud-controller-manager)
for general information on how to configure your tenant cluster to run `kubevirt-cloud-controller-manager`. You can
find example manifests for `kubevirt-cloud-controller-manager` in the [manifests directory](cluster/manifests) for
static Pod, Deployment and DaemonSet configurations.

To get it to run, you'll need to provide a kubeconfig for the infrastructure cluster to the `kubevirt-cloud-controller-manager` configuration.
The configuration should contain an `kubeconfig` key, like in the following example:
```shell
cat /etc/kubernetes/cloud/config
```
Output:
```yaml
kubeconfig: <infraKubeConfigPath>
loadBalancer:
  creationPollInterval: 5
  creationPollTimeout: 60
```

## How to build a Docker image
With `make image` you can build a [Docker image](build/images/kubevirt-cloud-controller-manager) containing `kubevirt-cloud-controller-manager`.

## Development
### Create a cloud config
First create a cloud config file in the project directory
```shell
touch dev/cloud-config
```
Next add a kubeconfig path to the cloud-config file.
The kubeconfig must point to the infrastructure cluster where KubeVirt is installed.
```shell
kubeconfig: <infraKubeConfigPath>
```
For more configuration options look at the
[cloud configuration](https://github.com/kubevirt/cloud-provider-kubevirt/blob/main/pkg/cloudprovider/kubevirt/cloud.go#L30) 

### Build KubeVirt CCM
Build `kubevirt-cloud-controller-manager` using `make build`. It will put the finished binary in
`bin/kubevirt-cloud-controller-manager`. 

### Run KubeVirt CCM
Run the following command:
```shell
bin/kubevirt-cloud-controller-manager --kubeconfig <path-to-tenant-cluster-kubeconfig> --cloud-config dev/cloud-config 
```
