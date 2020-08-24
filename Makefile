VERSION ?= v0.0.8
REGISTRY ?= dgonzalez

CLUSTER_NAME ?= kubevirt
KUBECONFIG := dev/kubeconfig
CLOUD_CONFIG := dev/cloud-config
CERT_DIR := dev/

.PHONY: all
all: clean test build

.PHONY: start
start:
	go run ./cmd/kubevirt-cloud-controller-manager \
		--kubeconfig=$(KUBECONFIG) \
		--cloud-provider=kubevirt \
		--use-service-account-credentials \
		--cloud-config=$(CLOUD_CONFIG) \
		--cluster-name=$(CLUSTER_NAME) \
		--cert-dir=$(CERT_DIR) \
		--v=2

.PHONY: clean
clean:
	if [ -d bin ]; then rm -r bin; fi
	GO111MODULE=on go clean -modcache -i -x ./...

.PHONY:deps-update
deps-update:
	GO111MODULE=on go mod tidy && GO111MODULE=on go mod vendor 

bin:
	mkdir bin

.PHONY: test
test:
	GO111MODULE=on go test ./pkg/...

.PHONY: build
build: bin
	GO111MODULE=on GOFLAGS=-mod=vendor GOOS=linux go build -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

.PHONY:image
image: build
	docker build -t $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION) -f build/images/kubevirt-cloud-controller-manager/Dockerfile .

.PHONY: push
push:
	docker push $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION)
