VERSION ?= main
REGISTRY ?= kubevirt

CLUSTER_NAME ?= kubevirt
KUBECONFIG := dev/kubeconfig
CLOUD_CONFIG := dev/cloud-config
CERT_DIR := dev/

export BIN_DIR := bin

GOLANGCI_LINT_VERSION ?= v1.52.2

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
	CGO_ENABLED=0 GOOS=linux go build -mod vendor -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

.PHONY:image
image:
	docker build -t $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION) -f build/images/kubevirt-cloud-controller-manager/Dockerfile .

.PHONY: push
push:
	docker push $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION)

.PHONY: generate
generate:
	go generate ./pkg/...

.PHONY: cluster-up
cluster-up:
	./kubevirtci up

.PHONY: cluster-sync
cluster-sync:
	./kubevirtci sync

.PHONY: cluster-down
cluster-down:
	./kubevirtci down

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=10m

.PHONY: functest
functest:
	./hack/functest.sh

.PHONY: build-e2e-test
build-e2e-test:
	./hack/build-e2e.sh

.PHONY: e2e-test
e2e-test: build-e2e-test
	./hack/run-e2e.sh
