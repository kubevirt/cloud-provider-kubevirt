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

### LoadBalancer Service field handling

When a tenant cluster creates a `Service` of type `LoadBalancer`, the provider
creates a mirrored `Service` in the infra cluster. Because the infra cluster is
more privileged than the tenant cluster, the provider only propagates the tenant
Service fields that are safe to expose across that trust boundary:

- `spec.ports` are mapped to tenant NodePorts. Infra-allocated NodePorts are
  retained during reconciliation. `spec.externalTrafficPolicy` is copied at creation.
- `spec.loadBalancerSourceRanges` is propagated (falling back to the legacy
  `service.beta.kubernetes.io/load-balancer-source-ranges` annotation) so that a
  tenant's source restriction is forwarded. Invalid ranges cause reconciliation
  to fail; enforcement depends on the infra load-balancer/network implementation.

The standard Kubernetes service controller skips tenant Services with
`spec.loadBalancerClass` set. Those Services need a separate controller; setting
the field does not select a backend for a mirrored infra Service.

The following tenant fields are **not** propagated by default:

- `metadata.annotations`: no tenant annotations are copied unless the infra
  operator allowlists specific keys via `loadBalancer.allowedAnnotations`.
  The provider's ownership annotation, `kubectl.kubernetes.io/last-applied-configuration`,
  and the legacy source-range annotation are never copied, even if allowlisted.
  Other keys, including provider-specific `service.beta.kubernetes.io/*` keys,
  may be explicitly allowlisted.
- `spec.externalIPs`: never copied (avoids CVE-2020-8554-style interception).
- `spec.healthCheckNodePort`: never copied; the infra Service manages its own
  node ports.
- `spec.loadBalancerIP`: ignored unless the infra operator opts in via
  `loadBalancer.allowTenantLoadBalancerIP`, so a tenant cannot select a specific
  address from the infra cluster's shared pool.

Ignored `externalIPs` and `loadBalancerIP` requests generate Warning events on
the tenant Service on creation and reconciliation.

#### Tenant event permissions

The recorder needs `create` and `patch` permissions on core API `events` in
every tenant namespace containing Services. The current client-go recorder uses
`create` for new events and `patch` for repeated events.

The identity to authorize depends on the CCM configuration:

- With shared credentials (the packaged Deployment's default), authorize the
  identity in the tenant `--kubeconfig`. The name passed to the client builder
  does not select a different identity in this mode.
- With `--use-service-account-credentials`, authorize the tenant
  `service-controller` ServiceAccount in the controller client builder's
  configured namespace (normally `kube-system`).

An event-only ClusterRole and example binding are provided in
[`config/tenant-rbac/events.yaml`](config/tenant-rbac/events.yaml). Adjust the
binding subject to the actual tenant identity and apply the file to the
**tenant** cluster. Existing CCM permissions remain necessary. This file is
deliberately separate from `config/rbac`, which is installed alongside the
provider in the infrastructure cluster; granting permissions there does not
authorize writes to the tenant API.

Check with the same tenant credentials the recorder uses, for example:

```shell
kubectl --kubeconfig=<tenant-ccm-kubeconfig> auth can-i create events --all-namespaces
kubectl --kubeconfig=<tenant-ccm-kubeconfig> auth can-i patch events --all-namespaces
```

Without these permissions, event writes are forbidden even though LoadBalancer
reconciliation can otherwise succeed.

#### Reconciliation and migration

Ports, source ranges, allowed annotations and the loadBalancerIP policy are
reconciled on update; existing infra `externalIPs` are cleared. Infra-allocated
health-check ports and any infra load-balancer class are preserved.

The infra-only `cloud-provider.kubevirt.io/tenant-annotation-keys` annotation
tracks provider-managed keys so that tenant removal or allowlist revocation
removes previously copied annotations without deleting other infra annotations.

**Upgrade:** older Services have no annotation ownership record. On first
reconciliation, only annotations whose keys and values still match the current
tenant Service are identified for cleanup. Historical annotations no longer
matching the tenant require an infra-operator audit and manual cleanup; matching
annotations independently set on both sides cannot be distinguished. Review
existing infra Services before upgrading. Clearing an IP request does not
guarantee that the infra controller releases an already allocated address, and
removing an annotation does not necessarily undo external side effects such as
DNS records. Audit those resources too. Existing health-check port allocations
are retained rather than reassigned during upgrade.

Example enabling a restricted annotation allowlist and tenant-requested IPs:
```yaml
kubeconfig: <infraKubeConfigPath>
loadBalancer:
  allowedAnnotations:
    - example.com/some-controller-key
  allowTenantLoadBalancerIP: true
```

> **Security note:** enabling `allowedAnnotations` (for example load-balancer
> IPAM keys) or `allowTenantLoadBalancerIP` lets a tenant cluster influence
> infra-cluster load-balancer behaviour, including selecting a specific address
> from the infra cluster's shared pool. Only enable these in single-tenant or
> otherwise trusted setups.

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
