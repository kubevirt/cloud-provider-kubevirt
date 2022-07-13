#!/bin/bash

set -e -o pipefail

echo "Building e2e test suite (TODO)"
#make build-e2e-test

echo "Starting and preparing kubevirtci cluster"
make cluster-up

echo "Building and installing cloud-provider-kubevirt manager container"
make cluster-sync

echo "Running e2e test suite (TODO)"
#export KUBECONFIG=$(./kubevirtci kubeconfig)
#export NODE_VM_IMAGE_TEMPLATE=quay.io/capk/ubuntu-container-disk:20.04
#export IMAGE_REPO=k8s.gcr.io
#export TENANT_CLUSTER_KUBERNETES_VERSION=v1.21.0
#export CRI_PATH=/var/run/containerd/containerd.sock
#export ROOT_VOLUME_SIZE=23Gi
#export STORAGE_CLASS_NAME=rook-ceph-block
#make e2e-test
