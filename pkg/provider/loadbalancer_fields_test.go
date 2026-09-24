package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	mockclient "kubevirt.io/cloud-provider-kubevirt/pkg/provider/mock/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestTenantSourceRanges(t *testing.T) {
	for _, tc := range []struct {
		name       string
		field      []string
		annotation string
		want       []string
		wantError  bool
	}{
		{name: "absent"},
		{name: "empty annotation", annotation: "  "},
		{name: "IPv4 and IPv6", annotation: "203.0.113.0/24,2001:db8::/32", want: []string{"2001:db8::/32", "203.0.113.0/24"}},
		{name: "field takes precedence", field: []string{"203.0.113.0/24"}, annotation: "invalid", want: []string{"203.0.113.0/24"}},
		{name: "empty entries", annotation: ",", wantError: true},
		{name: "trailing comma", annotation: "203.0.113.0/24,", wantError: true},
		{name: "invalid CIDR", annotation: "203.0.113.0/99", wantError: true},
		{name: "invalid field", field: []string{"invalid"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{corev1.AnnotationLoadBalancerSourceRangesKey: tc.annotation}},
				Spec:       corev1.ServiceSpec{LoadBalancerSourceRanges: tc.field},
			}
			before := svc.DeepCopy()
			got, err := tenantLoadBalancerSourceRanges(svc)
			if (err != nil) != tc.wantError || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v (error=%t)", got, err, tc.want, tc.wantError)
			}
			if !reflect.DeepEqual(svc, before) {
				t.Fatal("source Service mutated")
			}
		})
	}
}

func TestAnnotationOwnership(t *testing.T) {
	const allowed = "service.beta.kubernetes.io/aws-load-balancer-internal"
	lb := &loadbalancer{config: LoadBalancerConfig{AllowedAnnotations: []string{
		allowed, "notkubernetes.io/x", tenantAnnotationKeys, corev1.LastAppliedConfigAnnotation, corev1.AnnotationLoadBalancerSourceRangesKey,
	}}}
	tenant := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		allowed: "true", "notkubernetes.io/x": "value", tenantAnnotationKeys: `["infra.example/owned"]`,
		corev1.LastAppliedConfigAnnotation: "{}", corev1.AnnotationLoadBalancerSourceRangesKey: "203.0.113.0/24",
	}}}
	existing := map[string]string{"metallb.io/ip-allocated-from-pool": "pool", "infra.example/owned": "keep", tenantAnnotationKeys: "[]"}
	got, err := lb.reconcileAnnotations(tenant, existing)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		allowed: "true", "notkubernetes.io/x": "value", "metallb.io/ip-allocated-from-pool": "pool", "infra.example/owned": "keep",
		tenantAnnotationKeys: `["notkubernetes.io/x","service.beta.kubernetes.io/aws-load-balancer-internal"]`,
	}
	if !reflect.DeepEqual(got, want) || len(existing) != 3 {
		t.Fatalf("unexpected annotations or mutated input: %v, %v", got, existing)
	}
	// Changing a tenant value and removing another key updates only owned keys.
	tenant.Annotations = map[string]string{allowed: "false"}
	got, err = lb.reconcileAnnotations(tenant, got)
	if err != nil || got[allowed] != "false" || got["notkubernetes.io/x"] != "" || got["infra.example/owned"] != "keep" {
		t.Fatalf("update failed: %v, %v", got, err)
	}
	// Revoking the allowlist removes previously mirrored keys even if the tenant
	// has already removed them, but retains infra-owned annotations.
	lb.config.AllowedAnnotations = nil
	tenant.Annotations = nil
	got, err = lb.reconcileAnnotations(tenant, got)
	if err != nil || !reflect.DeepEqual(got, existing) {
		t.Fatalf("revocation failed: %v, %v", got, err)
	}
	again, err := lb.reconcileAnnotations(tenant, got)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Fatalf("reconciliation is not idempotent: %v, %v", again, err)
	}
}

func TestLegacyAnnotationMigration(t *testing.T) {
	lb := &loadbalancer{}
	tenant := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"tenant/key": "old", "infra/key": "different"}}}
	existing := map[string]string{"tenant/key": "old", "infra/key": "operator", "unknown/key": "keep"}
	got, err := lb.reconcileAnnotations(tenant, existing)
	want := map[string]string{"infra/key": "operator", "unknown/key": "keep", tenantAnnotationKeys: "[]"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected legacy migration: %v, %v", got, err)
	}
	if _, err := lb.reconcileAnnotations(tenant, map[string]string{tenantAnnotationKeys: "invalid"}); err == nil {
		t.Fatal("invalid ownership metadata accepted")
	}
}

func TestReconcileManagedFields(t *testing.T) {
	for _, ensure := range []bool{false, true} {
		name := "UpdateLoadBalancer"
		if ensure {
			name = "EnsureLoadBalancer"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			c := mockclient.NewMockClient(gomock.NewController(t))
			recorder := record.NewFakeRecorder(10)
			lb := &loadbalancer{client: c, namespace: "infra", recorder: recorder}
			tenant := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "workload", UID: "1234"},
				Spec: corev1.ServiceSpec{
					Ports:       []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001}},
					ExternalIPs: []string{"203.0.113.5"}, LoadBalancerIP: "203.0.113.6",
					LoadBalancerSourceRanges: []string{"203.0.113.0/24"},
				},
			}
			before := tenant.DeepCopy()
			infra := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "a1234", Namespace: "infra", Annotations: map[string]string{
					tenantAnnotationKeys: "[]", "metallb.io/ip-allocated-from-pool": "pool",
				}},
				Spec: corev1.ServiceSpec{
					Ports:       []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt(30001), NodePort: 31001}},
					ExternalIPs: []string{"203.0.113.5"}, LoadBalancerIP: "203.0.113.6",
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
					HealthCheckNodePort:   32001, LoadBalancerClass: pointer.String("infra.example/lb"),
				},
			}
			c.EXPECT().Get(ctx, client.ObjectKey{Name: "a1234", Namespace: "infra"}, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
					infra.DeepCopyInto(obj.(*corev1.Service))
					return nil
				}).Times(2)
			c.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
				infra = obj.(*corev1.Service).DeepCopy()
				return nil
			}).Times(1)
			for i := 0; i < 2; i++ {
				var err error
				if ensure {
					_, err = lb.EnsureLoadBalancer(ctx, "cluster", tenant, nil)
				} else {
					err = lb.UpdateLoadBalancer(ctx, "cluster", tenant, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(infra.Spec.ExternalIPs) != 0 || infra.Spec.LoadBalancerIP != "" ||
				infra.Spec.HealthCheckNodePort != 32001 || infra.Spec.Ports[0].NodePort != 31001 ||
				*infra.Spec.LoadBalancerClass != "infra.example/lb" ||
				!reflect.DeepEqual(infra.Spec.LoadBalancerSourceRanges, tenant.Spec.LoadBalancerSourceRanges) ||
				infra.Annotations["metallb.io/ip-allocated-from-pool"] != "pool" || !reflect.DeepEqual(tenant, before) {
				t.Fatalf("incorrect reconciliation: %+v", infra)
			}
			for _, reason := range []string{"ExternalIPsIgnored", "LoadBalancerIPIgnored", "ExternalIPsIgnored", "LoadBalancerIPIgnored"} {
				select {
				case event := <-recorder.Events:
					if !strings.Contains(event, "Warning "+reason) {
						t.Fatalf("unexpected event: %s", event)
					}
				default:
					t.Fatal("missing tenant warning")
				}
			}
		})
	}
}

func TestCreateManagedFields(t *testing.T) {
	c := mockclient.NewMockClient(gomock.NewController(t))
	recorder := record.NewFakeRecorder(10)
	lb := &loadbalancer{client: c, recorder: recorder}
	tenant := &corev1.Service{Spec: corev1.ServiceSpec{
		ExternalIPs: []string{"203.0.113.5"}, LoadBalancerIP: "203.0.113.6",
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal, HealthCheckNodePort: 32000,
	}}
	c.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
		svc := obj.(*corev1.Service)
		if svc.Spec.HealthCheckNodePort != 0 || len(svc.Spec.ExternalIPs) > 0 || svc.Spec.LoadBalancerIP != "" {
			t.Fatalf("tenant allocations copied: %+v", svc.Spec)
		}
		return nil
	})
	if _, err := lb.createLoadBalancerService(context.Background(), "infra", tenant, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Events) != 2 {
		t.Fatal("missing create warnings")
	}
	lb.config.AllowTenantLoadBalancerIP = pointer.Bool(true)
	tenant.Spec.ExternalIPs = nil
	lb.warnIgnoredFields(tenant)
	if len(recorder.Events) != 2 {
		t.Fatal("warning emitted for permitted IP")
	}
}

func TestInvalidSourceRangesPreventWrites(t *testing.T) {
	c := mockclient.NewMockClient(gomock.NewController(t)) // No API writes expected.
	lb := &loadbalancer{client: c}
	tenant := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{corev1.AnnotationLoadBalancerSourceRangesKey: ","}}}
	if _, err := lb.createLoadBalancerService(context.Background(), "infra", tenant, nil, nil, nil); err == nil {
		t.Fatal("invalid ranges accepted on create")
	}
	infra := &corev1.Service{Spec: corev1.ServiceSpec{LoadBalancerSourceRanges: []string{"203.0.113.0/24"}}}
	before := infra.DeepCopy()
	if err := lb.reconcileLoadBalancerService(context.Background(), tenant, infra, nil); err == nil || !reflect.DeepEqual(infra, before) {
		t.Fatal("invalid ranges must leave existing restrictions intact")
	}
}
