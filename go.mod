module kubevirt.io/cloud-provider-kubevirt

go 1.15

require (
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/go-logfmt/logfmt v0.4.0 // indirect
	github.com/golang/mock v1.2.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.17.0
	k8s.io/apimachinery v0.17.1-beta.0
	k8s.io/apiserver v0.16.8
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/cloud-provider v0.16.8
	k8s.io/component-base v0.17.0
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.16.8
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f // indirect
	kubevirt.io/client-go v0.26.5
)

replace (
	github.com/go-kit/kit => github.com/go-kit/kit v0.3.0
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2
	k8s.io/api => k8s.io/api v0.16.8 // 1.16.8
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.8 // 1.16.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.8 // 1.16.8
	k8s.io/apiserver => k8s.io/apiserver v0.16.8 // 1.16.8
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.16.8 // 1.16.8
	k8s.io/client-go => k8s.io/client-go v0.16.8 // 1.16.8
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.16.8 // 1.16.8
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.16.8 // 1.16.8
	k8s.io/code-generator => k8s.io/code-generator v0.16.8 // 1.16.8
	k8s.io/component-base => k8s.io/component-base v0.16.8 // 1.16.8
	k8s.io/cri-api => k8s.io/cri-api v0.16.8 // 1.16.8
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.16.8 // 1.16.8
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.16.8 // 1.16.8
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.16.8 // 1.16.8
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.16.8 // 1.16.8
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.16.8 // 1.16.8
	k8s.io/kubectl => k8s.io/kubectl v0.16.8 // 1.16.8
	k8s.io/kubelet => k8s.io/kubelet v0.16.8 // 1.16.8
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.16.8 // 1.16.8
	k8s.io/metrics => k8s.io/metrics v0.16.8 // 1.16.8
	k8s.io/node-api => k8s.io/node-api v0.16.8 // 1.16.8
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.16.8 // 1.16.8
)
