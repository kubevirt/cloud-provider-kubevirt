# cloud-provider-kubevirt
The KubeVirt cloud-provider allows you to use KubeVirt and Kubernetes as a "cloud" to run Kubernetes clusters on top.
This project contains the `kubevirt-cloud-controller-manager`, an implementation of the cloud controller manager (see
[Concepts Underlying the Cloud Controller Manager](https://kubernetes.io/docs/concepts/architecture/cloud-controller/)
for more details).

## Introduction
The KubeVirt cloud-provider allows a Kubernetes cluster running in KubeVirt VMs (lets call this cluster "OverKube") to
interact with KubeVirt and Kubernetes ("UnderKube") to provision, manage and clean up resources. For example, the
cloud-provider ensures that [zone and region
labels](https://kubernetes.io/docs/reference/kubernetes-api/labels-annotations-taints/#failure-domainbetakubernetesiozone)
of nodes in the OverKube are set based on the zone and region of the KubeVirt VMs in the UnderKube. The cloud-provider
also ensures that OverKube services of type
[LoadBalancer](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) are properly exposed
through services in the UnderKube.

## How to run `kubevirt-cloud-controller-manager`
See [Running cloud-controller-manager](https://kubernetes.io/docs/tasks/administer-cluster/running-cloud-controller/#running-cloud-controller-manager)
for general information on how to configure your OverKube cluster to run `kubevirt-cloud-controller-manager`. You can
find example manifests for `kubevirt-cloud-controller-manager` in the [manifests directory](cluster/manifests) for
static Pod, Deployment and DaemonSet configurations.

To get it to run, you'll need to provide a kubeconfig for the UnderKube to the `kubevirt-cloud-controller-manager` as a
secret. You can create the secret with the following command: `kubectl -n kube-system create secret generic
cloud-config --from-file=cloud-config=kubeconfig.yaml`, where `kubeconfig.yaml` contains a valid configuration to access
the UnderKube. After the secret is in place, you can deploy `kubevirt-cloud-controller-manager` using:
```
kubectl apply -f https://raw.githubusercontent.com/gonzolino/cloud-provider-kubevirt/master/cluster/manifests/kubevirt-cloud-controller-manager-ds.yaml
```

## Development
You can build `kubevirt-cloud-controller-manager` using `make build`. It will put the finished binary in
`bin/kubevirt-cloud-controller-manager`. With `make image` you can build a [Docker
image](build/images/kubevirt-cloud-controller-manager) containing
`kubevirt-cloud-controller-manager`.

This project currently uses [Glide](https://github.com/Masterminds/glide) for dependency management. Dependencies are
automatically installed when running `make build`. If you need to update dependencies, run `make deps-update`.