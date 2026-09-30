package provider

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
)

type tenantEventClientBuilder struct {
	cloudprovider.ControllerClientBuilder
	client clientset.Interface
}

func (b tenantEventClientBuilder) ClientOrDie(string) clientset.Interface {
	return b.client
}

func TestInitializeTenantEvents(t *testing.T) {
	tenant := fake.NewClientset()
	cloud := &Cloud{config: createDefaultCloudConfig()}
	stop := make(chan struct{})
	defer close(stop)
	cloud.Initialize(tenantEventClientBuilder{client: tenant}, stop)
	balancer, enabled := cloud.LoadBalancer()
	if !enabled {
		t.Fatal("load balancer disabled")
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "tenant", UID: "service-uid"},
		Spec:       corev1.ServiceSpec{LoadBalancerIP: "203.0.113.1"},
	}
	balancer.(*loadbalancer).warnIgnoredFields(svc)
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("Warning event was not written using tenant client")
		case <-ticker.C:
			events, err := tenant.CoreV1().Events("tenant").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events.Items {
				if event.Reason == "LoadBalancerIPIgnored" && event.Type == corev1.EventTypeWarning && event.InvolvedObject.UID == svc.UID {
					return
				}
			}
		}
	}
}
