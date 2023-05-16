#!/bin/bash

set -e -o pipefail

export TENANT_CLUSTER_NAME=${TENANT_CLUSTER_NAME:-kvcluster}
export TENANT_CLUSTER_NAMESPACE=${TENANT_CLUSTER_NAMESPACE:-kvcluster}
export TENANT_KUBECONFIG="config/secret/kubeconfig"
export INFRA_KUBECONFIG=$(./kubevirtci kubeconfig)
export KUBECONFIG=$(./kubevirtci kubeconfig)
DUMP_VERSION=$(curl -L https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/latest)
DUMP_PATH=bin/kubevirt-${DUMP_VERSION}-dump
export ARTIFACTS=${ARTIFACTS:-k8s-reporter}
mkdir -p $ARTIFACTS

if [ ! -f "$DUMP_PATH" ]; then
    curl -L https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${DUMP_VERSION}/testing/dump -o $DUMP_PATH
    chmod 755 $DUMP_PATH
fi

virtctl_path=./hack/tools/bin/virtctl

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

${virtctl_path} port-forward -n ${TENANT_CLUSTER_NAMESPACE} vm/${control_plane_vm_name} 64443:6443 > /dev/null 2>&1 &
trap 'kill $! > /dev/null 2>&1' EXIT SIGSTOP SIGKILL SIGTERM

kubectl --kubeconfig ${TENANT_KUBECONFIG} config set-cluster ${TENANT_CLUSTER_NAME} --server=https://localhost:64443 --insecure-skip-tls-verify=true
kubectl --kubeconfig ${TENANT_KUBECONFIG} config unset clusters.${TENANT_CLUSTER_NAME}.certificate-authority-data

DUMP_PATH=$DUMP_PATH $BIN_DIR/e2e.test
