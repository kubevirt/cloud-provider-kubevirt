VERSION ?= v0.1.0
REGISTRY ?= kubevirt

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
	go clean -modcache -i -x ./...

.PHONY:deps-update
deps-update:
	go mod tidy

bin:
	mkdir bin

.PHONY: test
test:
	go test ./pkg/...

.PHONY: build
build: bin
	CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

.PHONY:image
image: build
	docker build -t $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION) -f build/images/kubevirt-cloud-controller-manager/Dockerfile .

.PHONY: push
push:
	docker push $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION)

.PHONY: generate
generate:
	go generate ./pkg/...
