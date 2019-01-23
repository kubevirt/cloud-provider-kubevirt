FROM alpine:3.8

RUN apk add --no-cache ca-certificates
ADD bin/kubevirt-cloud-controller-manager /bin/

ENTRYPOINT ["/bin/kubevirt-cloud-controller-manager"]
