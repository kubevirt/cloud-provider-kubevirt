FROM --platform=linux/amd64 golang:1.20.6 AS builder

WORKDIR /go/src/kubevirt.io/cloud-provider-kubevirt

COPY go.* ./

COPY pkg/ pkg/
COPY cmd/ cmd/
COPY vendor/ vendor/

RUN	CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

FROM registry.access.redhat.com/ubi9/ubi-micro
COPY --from=builder /go/src/kubevirt.io/cloud-provider-kubevirt/bin/kubevirt-cloud-controller-manager /bin/kubevirt-cloud-controller-manager
ENTRYPOINT [ "/bin/kubevirt-cloud-controller-manager" ]
