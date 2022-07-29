#!/bin/bash

set -e -o pipefail

export TENANT_CLUSTER_NAME=${TENANT_CLUSTER_NAME:-kvcluster}
export TENANT_CLUSTER_NAMESPACE=${TENANT_CLUSTER_NAMESPACE:-kvcluster}
export KUBECONFIG=$(./kubevirtci kubeconfig)

sonobuoy_release="https://github.com/vmware-tanzu/sonobuoy/releases/download/v0.56.8/sonobuoy_0.56.8_linux_amd64.tar.gz"

vms_list=$(kubectl get vm -n ${TENANT_CLUSTER_NAMESPACE} --no-headers -o custom-columns=":metadata.name")
for vm in $vms_list; do
    if [[ "$vm" == ${TENANT_CLUSTER_NAME}-control-plane* ]]; then
            control_plane_vm_name=$vm
    fi
done

if [ -n "${control_plane_vm_name}" ]; then
    echo "Found control plane VM: ${control_plane_vm_name} in namespace ${TENANT_CLUSTER_NAMESPACE}"
else
    echo "control-plane vm is not found in namespace ${TENANT_CLUSTER_NAMESPACE} (looking for regex ${TENANT_CLUSTER_NAME}-control-plane*)"
    exit 1
fi

./kubevirtci ssh-tenant $control_plane_vm_name $TENANT_CLUSTER_NAMESPACE -- \
"sudo curl -L $sonobuoy_release -o sonobuoy.tar.gz && \
tar -xzf sonobuoy.tar.gz && \
chmod +x sonobuoy && \
sudo KUBECONFIG=/etc/kubernetes/admin.conf ./sonobuoy run --plugin e2e --wait"
trap './kubevirtci ssh-tenant $control_plane_vm_name $TENANT_CLUSTER_NAMESPACE -- "sudo KUBECONFIG=/etc/kubernetes/admin.conf ./sonobuoy delete"' EXIT SIGSTOP SIGKILL SIGTERM

retrieve_file=$(./kubevirtci ssh-tenant $control_plane_vm_name $TENANT_CLUSTER_NAMESPACE -- "sudo KUBECONFIG=/etc/kubernetes/admin.conf ./sonobuoy retrieve" | tail -1 | awk '{ print $NF }')

./kubevirtci scp-tenant $control_plane_vm_name $TENANT_CLUSTER_NAMESPACE $retrieve_file
