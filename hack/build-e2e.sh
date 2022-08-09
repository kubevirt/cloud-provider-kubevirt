#!/bin/bash

set -ex

rm -rf $BIN_DIR
mkdir -p $BIN_DIR
go test ./test/e2e/cloud_provider_kubevirt/ -c -o "$BIN_DIR/e2e.test"
