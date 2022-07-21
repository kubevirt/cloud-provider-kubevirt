package resources

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func CurlLoadBalancerJob(name, ip, port string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "alpine",
							Image: "quay.io/rhrazdil/alpine:v0.1",
							Command: []string{
								"/bin/sh",
							},
							Args: []string{
								"-c",
								fmt.Sprintf("apk add curl && curl %s:%s", ip, port),
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			BackoffLimit: pointer.Int32(4),
		},
	}
}
