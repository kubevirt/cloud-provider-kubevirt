FROM --platform=linux/amd64 golang:1.17.11 AS builder

WORKDIR /go/src/kubevirt.io/cloud-provider-kubevirt
COPY . .

RUN make build

FROM registry.access.redhat.com/ubi9/ubi-micro
COPY --from=builder /go/src/kubevirt.io/cloud-provider-kubevirt/bin/kubevirt-cloud-controller-manager /bin/kubevirt-cloud-controller-manager
ENTRYPOINT [ "/bin/kubevirt-cloud-controller-manager" ]
