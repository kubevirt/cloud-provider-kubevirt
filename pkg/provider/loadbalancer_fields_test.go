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
	got := lb.reconcileAnnotations(tenant, existing, true, false)
	want := map[string]string{
		allowed: "true", "notkubernetes.io/x": "value", "metallb.io/ip-allocated-from-pool": "pool", "infra.example/owned": "keep",
		tenantAnnotationKeys: `["notkubernetes.io/x","service.beta.kubernetes.io/aws-load-balancer-internal"]`,
	}
	if !reflect.DeepEqual(got, want) || len(existing) != 3 {
		t.Fatalf("unexpected annotations or mutated input: %v, %v", got, existing)
	}
	// Changing a tenant value and removing another key updates only owned keys.
	tenant.Annotations = map[string]string{allowed: "false"}
	got = lb.reconcileAnnotations(tenant, got, true, false)
	if got[allowed] != "false" || got["notkubernetes.io/x"] != "" || got["infra.example/owned"] != "keep" {
		t.Fatalf("update failed: %v", got)
	}
	// Revoking the allowlist removes previously mirrored keys even if the tenant
	// has already removed them, but retains infra-owned annotations.
	lb.config.AllowedAnnotations = nil
	tenant.Annotations = nil
	got = lb.reconcileAnnotations(tenant, got, true, false)
	if !reflect.DeepEqual(got, existing) {
		t.Fatalf("revocation failed: %v", got)
	}
	again := lb.reconcileAnnotations(tenant, got, true, false)
	if !reflect.DeepEqual(again, got) {
		t.Fatalf("reconciliation is not idempotent: %v", again)
	}
}

func TestLegacyAnnotationMigration(t *testing.T) {
	lb := &loadbalancer{}
	tenant := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"tenant/key": "old", "infra/key": "different"}}}
	existing := map[string]string{"tenant/key": "old", "infra/key": "operator", "unknown/key": "keep"}
	got := lb.reconcileAnnotations(tenant, existing, false, false)
	want := map[string]string{"infra/key": "operator", "unknown/key": "keep", tenantAnnotationKeys: "[]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected legacy migration: %v", got)
	}
	// A corrupt record on a labelled Service falls back to the legacy path instead of failing.
	got = lb.reconcileAnnotations(tenant, map[string]string{tenantAnnotationKeys: "invalid", "tenant/key": "old"}, true, false)
	if !reflect.DeepEqual(got, map[string]string{tenantAnnotationKeys: "[]"}) {
		t.Fatalf("invalid ownership metadata not ignored: %v", got)
	}
}

// Older builds copied every tenant annotation onto the infra Service, so a tenant
// could plant the ownership record. Without the ownership label it must be ignored.
func TestPlantedOwnershipRecordIgnored(t *testing.T) {
	for _, planted := range []string{"x", "[]", `["metallb.io/ip-allocated-from-pool"]`} {
		t.Run(planted, func(t *testing.T) {
			ctx := context.Background()
			c := mockclient.NewMockClient(gomock.NewController(t))
			lb := &loadbalancer{client: c, namespace: "infra"}
			tenantAnnotations := map[string]string{
				tenantAnnotationKeys:                  planted,
				"metallb.universe.tf/loadBalancerIPs": "203.0.113.6",
			}
			tenant := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "workload", UID: "1234", Annotations: tenantAnnotations},
				Spec:       corev1.ServiceSpec{ExternalIPs: []string{"203.0.113.5"}, LoadBalancerIP: "203.0.113.6"},
			}
			// Built the way the old createLoadBalancerService did: tenant annotations and IPs copied.
			infra := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "a1234", Namespace: "infra", Annotations: map[string]string{
					tenantAnnotationKeys:                  planted,
					"metallb.universe.tf/loadBalancerIPs": "203.0.113.6",
					"metallb.io/ip-allocated-from-pool":   "pool",
				}},
				Spec: corev1.ServiceSpec{ExternalIPs: []string{"203.0.113.5"}, LoadBalancerIP: "203.0.113.6"},
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
				if _, err := lb.EnsureLoadBalancer(ctx, "cluster", tenant, nil); err != nil {
					t.Fatal(err)
				}
			}
			want := map[string]string{tenantAnnotationKeys: "[]", "metallb.io/ip-allocated-from-pool": "pool"}
			if !reflect.DeepEqual(infra.Annotations, want) || len(infra.Spec.ExternalIPs) != 0 || infra.Spec.LoadBalancerIP != "" ||
				infra.Labels[annotationOwnershipLabel] != annotationOwnershipLabelValue {
				t.Fatalf("planted record not ignored: %+v", infra)
			}
		})
	}
}

func TestReconcileManagedFields(t *testing.T) {
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
		}, Labels: map[string]string{annotationOwnershipLabel: annotationOwnershipLabelValue}},
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
		_, err := lb.EnsureLoadBalancer(ctx, "cluster", tenant, nil)
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
		if svc.Labels[annotationOwnershipLabel] != annotationOwnershipLabelValue {
			t.Fatalf("ownership label missing on create: %v", svc.Labels)
		}
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

func TestStaleNodeUpdateDoesNotWriteService(t *testing.T) {
	lb := &loadbalancer{client: mockclient.NewMockClient(gomock.NewController(t))}
	stale := &corev1.Service{Spec: corev1.ServiceSpec{LoadBalancerIP: "203.0.113.5"}}
	if err := lb.UpdateLoadBalancer(context.Background(), "cluster", stale, []*corev1.Node{{}}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSourceRangesStillRevoke(t *testing.T) {
	c := mockclient.NewMockClient(gomock.NewController(t))
	lb := &loadbalancer{client: c, config: LoadBalancerConfig{AllowedAnnotations: []string{"tenant/new", "tenant/keep"}}}
	tenant := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		corev1.AnnotationLoadBalancerSourceRangesKey: ",", "tenant/revoked": "old", "tenant/new": "new", "tenant/keep": "changed",
	}}, Spec: corev1.ServiceSpec{LoadBalancerIP: "203.0.113.5"}}
	if _, err := lb.createLoadBalancerService(context.Background(), "infra", tenant, nil, nil, nil); err == nil {
		t.Fatal("invalid ranges accepted on create")
	}
	infra := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{annotationOwnershipLabel: annotationOwnershipLabelValue},
		Annotations: map[string]string{tenantAnnotationKeys: `["tenant/revoked","tenant/keep"]`, "tenant/revoked": "old", "tenant/keep": "old", "infra/key": "keep"},
	}, Spec: corev1.ServiceSpec{LoadBalancerSourceRanges: []string{"203.0.113.0/24"}, ExternalIPs: []string{"203.0.113.6"}, LoadBalancerIP: tenant.Spec.LoadBalancerIP}}
	want := infra.DeepCopy()
	delete(want.Annotations, "tenant/revoked")
	want.Annotations[tenantAnnotationKeys] = `["tenant/keep"]`
	want.Spec.ExternalIPs = nil
	want.Spec.LoadBalancerIP = ""
	c.EXPECT().Update(context.Background(), want)
	for i := 0; i < 2; i++ {
		if err := lb.reconcileLoadBalancerService(context.Background(), tenant, infra, nil, []corev1.ServicePort{{Port: 443}}); err == nil {
			t.Fatal("expected source range error even after cleanup")
		}
	}
}

func TestReconcileExternalTrafficPolicy(t *testing.T) {
	vmiLabels := map[string]string{TenantNodeRoleLabelKey: "worker", TenantClusterNameLabelKey: "cluster"}
	for _, tc := range []struct {
		name         string
		config       LoadBalancerConfig
		tenantPolicy corev1.ServiceExternalTrafficPolicy
		infraPolicy  corev1.ServiceExternalTrafficPolicy
		infraSel     map[string]string
		wantPolicy   corev1.ServiceExternalTrafficPolicy
		wantSel      map[string]string
		wantUpdate   bool
		wantWarning  bool
	}{
		{
			name:         "tenant switches Local to Cluster",
			tenantPolicy: corev1.ServiceExternalTrafficPolicyCluster,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyLocal,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyCluster,
			wantSel:      vmiLabels,
			wantUpdate:   true,
		},
		{
			name:         "tenant switches Cluster to Local",
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyCluster,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyLocal,
			wantSel:      vmiLabels,
			wantUpdate:   true,
		},
		{
			name:         "unchanged policy is not updated",
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyLocal,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyLocal,
			wantSel:      vmiLabels,
		},
		{
			name:         "tenant switches to Local with the EPS controller removes the selector",
			config:       LoadBalancerConfig{EnableEPSController: pointer.Bool(true)},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyCluster,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyLocal,
			wantUpdate:   true,
		},
		{
			name:         "tenant switches to Cluster with the EPS controller restores the selector",
			config:       LoadBalancerConfig{EnableEPSController: pointer.Bool(true)},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyCluster,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyLocal,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyCluster,
			wantSel:      vmiLabels,
			wantUpdate:   true,
		},
		{
			name:         "operator forces Cluster over a Local tenant",
			config:       LoadBalancerConfig{ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyCluster},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyLocal,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyCluster,
			wantSel:      vmiLabels,
			wantUpdate:   true,
			wantWarning:  true,
		},
		{
			name:         "operator forced Cluster is stable",
			config:       LoadBalancerConfig{ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyCluster},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyCluster,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyCluster,
			wantSel:      vmiLabels,
			wantWarning:  true,
		},
		{
			name:         "forced Cluster keeps following the tenant policy for the selector",
			config:       LoadBalancerConfig{ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyCluster, EnableEPSController: pointer.Bool(true)},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyCluster,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyCluster,
			wantWarning:  true,
		},
		{
			name:         "operator forces Local over a Cluster tenant",
			config:       LoadBalancerConfig{ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal},
			tenantPolicy: corev1.ServiceExternalTrafficPolicyCluster,
			infraPolicy:  corev1.ServiceExternalTrafficPolicyCluster,
			infraSel:     vmiLabels,
			wantPolicy:   corev1.ServiceExternalTrafficPolicyLocal,
			wantSel:      vmiLabels,
			wantUpdate:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			c := mockclient.NewMockClient(gomock.NewController(t))
			recorder := record.NewFakeRecorder(10)
			lb := &loadbalancer{client: c, namespace: "infra", recorder: recorder, config: tc.config}
			tenant := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "workload", UID: "1234"},
				Spec: corev1.ServiceSpec{
					Ports:                 []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, NodePort: 30001}},
					ExternalTrafficPolicy: tc.tenantPolicy,
				},
			}
			infra := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "a1234", Namespace: "infra",
					Annotations: map[string]string{tenantAnnotationKeys: "[]"},
					Labels:      map[string]string{annotationOwnershipLabel: annotationOwnershipLabelValue}},
				Spec: corev1.ServiceSpec{
					Ports:                 []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt(30001)}},
					ExternalTrafficPolicy: tc.infraPolicy,
					Selector:              tc.infraSel,
				},
			}
			c.EXPECT().Get(ctx, client.ObjectKey{Name: "a1234", Namespace: "infra"}, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
					infra.DeepCopyInto(obj.(*corev1.Service))
					return nil
				})
			if tc.wantUpdate {
				c.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					infra = obj.(*corev1.Service).DeepCopy()
					return nil
				})
			}
			if _, err := lb.EnsureLoadBalancer(ctx, "cluster", tenant, nil); err != nil {
				t.Fatal(err)
			}
			if infra.Spec.ExternalTrafficPolicy != tc.wantPolicy {
				t.Fatalf("externalTrafficPolicy = %q, want %q", infra.Spec.ExternalTrafficPolicy, tc.wantPolicy)
			}
			if !reflect.DeepEqual(infra.Spec.Selector, tc.wantSel) {
				t.Fatalf("selector = %v, want %v", infra.Spec.Selector, tc.wantSel)
			}
			if tc.wantWarning {
				select {
				case event := <-recorder.Events:
					if !strings.Contains(event, "Warning ExternalTrafficPolicyOverridden") {
						t.Fatalf("unexpected event: %s", event)
					}
				default:
					t.Fatal("missing tenant warning")
				}
			} else if len(recorder.Events) != 0 {
				t.Fatalf("unexpected event: %s", <-recorder.Events)
			}
		})
	}
}
