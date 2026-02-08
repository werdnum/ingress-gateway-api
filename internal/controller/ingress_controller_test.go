package controller

import (
	"context"
	"testing"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/werdnum/ingress-gateway-api/internal/config"
	"github.com/werdnum/ingress-gateway-api/internal/converter"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = gatewayv1.Install(scheme)
	_ = gatewayv1beta1.Install(scheme)
	_ = egv1alpha1.AddToScheme(scheme)
	return scheme
}

func TestIngressReconciler_ShouldProcess(t *testing.T) {
	tests := []struct {
		name         string
		ingressClass string
		filterClass  string
		expected     bool
	}{
		{
			name:         "no filter processes all",
			ingressClass: "nginx",
			filterClass:  "",
			expected:     true,
		},
		{
			name:         "matching class",
			ingressClass: "nginx",
			filterClass:  "nginx",
			expected:     true,
		},
		{
			name:         "non-matching class",
			ingressClass: "nginx",
			filterClass:  "traefik",
			expected:     false,
		},
		{
			name:         "no ingress class with filter",
			ingressClass: "",
			filterClass:  "nginx",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				IngressClass: tt.filterClass,
			}

			r := &IngressReconciler{
				Config: cfg,
			}

			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			}
			if tt.ingressClass != "" {
				ingress.Spec.IngressClassName = &tt.ingressClass
			}

			result := r.shouldProcess(ingress)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIngressReconciler_GetIngressClass(t *testing.T) {
	tests := []struct {
		name           string
		ingress        *networkingv1.Ingress
		expectedClass  string
	}{
		{
			name: "spec ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: ptr("nginx"),
				},
			},
			expectedClass: "nginx",
		},
		{
			name: "annotation ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "traefik",
					},
				},
			},
			expectedClass: "traefik",
		},
		{
			name: "spec takes precedence over annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "traefik",
					},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: ptr("nginx"),
				},
			},
			expectedClass: "nginx",
		},
		{
			name: "no ingress class",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			expectedClass: "",
		},
	}

	r := &IngressReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.getIngressClass(tt.ingress)
			if result != tt.expectedClass {
				t.Errorf("expected %q, got %q", tt.expectedClass, result)
			}
		})
	}
}

func TestIngressReconciler_Reconcile_CreatesHTTPRoute(t *testing.T) {
	scheme := setupScheme()

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ingress",
			Namespace:  "default",
			UID:        types.UID("test-uid"),
			Finalizers: []string{FinalizerName},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api",
									PathType: ptr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "envoy-gateway",
	}

	conv := converter.New(cfg)

	r := &IngressReconciler{
		Client:    client,
		Scheme:    scheme,
		Config:    cfg,
		Converter: conv,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingress",
			Namespace: "default",
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue")
	}

	// Verify HTTPRoute was created
	var httpRoutes gatewayv1.HTTPRouteList
	if err := client.List(ctx, &httpRoutes); err != nil {
		t.Fatalf("failed to list HTTPRoutes: %v", err)
	}

	if len(httpRoutes.Items) != 1 {
		t.Fatalf("expected 1 HTTPRoute, got %d", len(httpRoutes.Items))
	}

	route := httpRoutes.Items[0]
	if route.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", route.Namespace)
	}
	if len(route.Spec.ParentRefs) != 1 {
		t.Errorf("expected 1 parent ref, got %d", len(route.Spec.ParentRefs))
	}
	if route.Spec.ParentRefs[0].Name != "test-gateway" {
		t.Errorf("expected gateway name test-gateway, got %s", route.Spec.ParentRefs[0].Name)
	}
}

func TestIngressReconciler_Reconcile_SkipsNonMatchingClass(t *testing.T) {
	scheme := setupScheme()

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: "default",
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ptr("traefik"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "envoy-gateway",
		IngressClass:     "nginx",
	}

	conv := converter.New(cfg)

	r := &IngressReconciler{
		Client:    client,
		Scheme:    scheme,
		Config:    cfg,
		Converter: conv,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingress",
			Namespace: "default",
		},
	}

	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue")
	}

	// Verify no HTTPRoute was created
	var httpRoutes gatewayv1.HTTPRouteList
	if err := client.List(ctx, &httpRoutes); err != nil {
		t.Fatalf("failed to list HTTPRoutes: %v", err)
	}

	if len(httpRoutes.Items) != 0 {
		t.Errorf("expected 0 HTTPRoutes, got %d", len(httpRoutes.Items))
	}
}

func TestIngressReconciler_Reconcile_CleansUpStaleResources(t *testing.T) {
	scheme := setupScheme()

	// Create Ingress with backend-protocol: HTTPS annotation
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ingress",
			Namespace:  "default",
			UID:        types.UID("test-uid"),
			Finalizers: []string{FinalizerName},
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api",
									PathType: ptr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 443,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "envoy-gateway",
	}

	conv := converter.New(cfg)

	r := &IngressReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Config:    cfg,
		Converter: conv,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingress",
			Namespace: "default",
		},
	}

	// First reconcile - should create BackendTLSPolicy
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on first reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue on first reconcile")
	}

	// Verify BackendTLSPolicy was created
	var btlsList gatewayv1.BackendTLSPolicyList
	if err := fakeClient.List(ctx, &btlsList); err != nil {
		t.Fatalf("failed to list BackendTLSPolicies: %v", err)
	}
	if len(btlsList.Items) != 1 {
		t.Fatalf("expected 1 BackendTLSPolicy, got %d", len(btlsList.Items))
	}

	// Remove the annotation from the Ingress
	if err := fakeClient.Get(ctx, req.NamespacedName, ingress); err != nil {
		t.Fatalf("failed to get ingress: %v", err)
	}
	delete(ingress.Annotations, "nginx.ingress.kubernetes.io/backend-protocol")
	if err := fakeClient.Update(ctx, ingress); err != nil {
		t.Fatalf("failed to update ingress: %v", err)
	}

	// Second reconcile - should delete the stale BackendTLSPolicy
	result, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on second reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue on second reconcile")
	}

	// Verify BackendTLSPolicy was deleted
	if err := fakeClient.List(ctx, &btlsList); err != nil {
		t.Fatalf("failed to list BackendTLSPolicies after cleanup: %v", err)
	}
	if len(btlsList.Items) != 0 {
		t.Errorf("expected 0 BackendTLSPolicies after cleanup, got %d", len(btlsList.Items))
	}
}

func TestIngressReconciler_Reconcile_CleansUpStaleSecurityPolicy(t *testing.T) {
	scheme := setupScheme()

	// Create Ingress with CORS annotation
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-ingress",
			Namespace:  "default",
			UID:        types.UID("test-uid"),
			Finalizers: []string{FinalizerName},
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/enable-cors": "true",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api",
									PathType: ptr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ingress).
		Build()

	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "envoy-gateway",
	}

	conv := converter.New(cfg)

	r := &IngressReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Config:    cfg,
		Converter: conv,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-ingress",
			Namespace: "default",
		},
	}

	// First reconcile - should create SecurityPolicy
	result, err := r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on first reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue on first reconcile")
	}

	// Verify SecurityPolicy was created
	var spList egv1alpha1.SecurityPolicyList
	if err := fakeClient.List(ctx, &spList); err != nil {
		t.Fatalf("failed to list SecurityPolicies: %v", err)
	}
	if len(spList.Items) != 1 {
		t.Fatalf("expected 1 SecurityPolicy, got %d", len(spList.Items))
	}

	// Remove the annotation from the Ingress
	if err := fakeClient.Get(ctx, req.NamespacedName, ingress); err != nil {
		t.Fatalf("failed to get ingress: %v", err)
	}
	delete(ingress.Annotations, "nginx.ingress.kubernetes.io/enable-cors")
	if err := fakeClient.Update(ctx, ingress); err != nil {
		t.Fatalf("failed to update ingress: %v", err)
	}

	// Second reconcile - should delete the stale SecurityPolicy
	result, err = r.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on second reconcile: %v", err)
	}
	if result.Requeue {
		t.Error("unexpected requeue on second reconcile")
	}

	// Verify SecurityPolicy was deleted
	if err := fakeClient.List(ctx, &spList); err != nil {
		t.Fatalf("failed to list SecurityPolicies after cleanup: %v", err)
	}
	if len(spList.Items) != 0 {
		t.Errorf("expected 0 SecurityPolicies after cleanup, got %d", len(spList.Items))
	}
}

func ptr[T any](v T) *T {
	return &v
}
