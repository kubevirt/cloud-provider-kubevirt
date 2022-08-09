#!/bin/bash

set -ex -o pipefail

echo "Building e2e test suite"
make build-e2e-test

echo "Starting kubevirtci cluster"
make cluster-up

echo "Building and installing cloud-provider-kubevirt manager container"
make cluster-sync

echo "Running e2e test suite"
make e2e-test
