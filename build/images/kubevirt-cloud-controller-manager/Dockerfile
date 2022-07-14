FROM --platform=linux/amd64 golang:1.17.11 AS builder

WORKDIR /go/src/kubevirt.io/cloud-provider-kubevirt

COPY go.* ./
RUN go mod download

COPY pkg/ pkg/
COPY cmd/ cmd/
RUN	CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o bin/kubevirt-cloud-controller-manager ./cmd/kubevirt-cloud-controller-manager

FROM registry.redhat.io/ubi9/ubi-micro
COPY --from=builder /go/src/kubevirt.io/cloud-provider-kubevirt/bin/kubevirt-cloud-controller-manager /bin/kubevirt-cloud-controller-manager
ENTRYPOINT [ "/bin/kubevirt-cloud-controller-manager" ]
