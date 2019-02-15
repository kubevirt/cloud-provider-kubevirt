VERSION ?= v0.0.7
REGISTRY ?= dgonzalez

.PHONY: all
all: clean deps-install test build

.PHONY: clean
clean:
	if [ -d bin ]; then rm -r bin; fi
	go clean -i -x ./...

.PHONY:deps-install
deps-install:
	glide install -v

.PHONY:deps-update
deps-update:
	glide update -v

bin:
	mkdir bin

.PHONY: test
test:
	go test ./pkg/...

.PHONY: build
build: bin
	GOOS=linux go build -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

.PHONY:image
image: build
	docker build -t $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION) -f build/images/kubevirt-cloud-controller-manager/Dockerfile .

.PHONY: push
push:
	docker push $(REGISTRY)/kubevirt-cloud-controller-manager:$(VERSION)
