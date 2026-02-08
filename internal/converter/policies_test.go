package converter

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/annotations"
	"github.com/werdnum/ingress-gateway-api/internal/config"
)

func TestGenerateBackendTrafficPolicy(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "eg-gateway",
		GatewayNamespace: "envoy-gateway",
	}
	c := New(cfg)

	tests := []struct {
		name          string
		annotations   map[string]string
		wantPolicy    bool
		wantTimeout   bool
		wantLB        bool
		wantBufferLim bool
	}{
		{
			name:        "no annotations",
			annotations: map[string]string{},
			wantPolicy:  false,
		},
		{
			name: "timeout annotations",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/proxy-read-timeout": "60",
			},
			wantPolicy:  true,
			wantTimeout: true,
		},
		{
			name: "load balancer annotation",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/upstream-hash-by": "$remote_addr",
			},
			wantPolicy: true,
			wantLB:     true,
		},
		{
			name: "body size annotation",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/proxy-body-size": "10m",
			},
			wantPolicy:    true,
			wantBufferLim: true,
		},
		{
			name: "multiple annotations",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/proxy-read-timeout": "60",
				"nginx.ingress.kubernetes.io/upstream-hash-by":   "$cookie_session",
				"nginx.ingress.kubernetes.io/proxy-body-size":    "10m",
			},
			wantPolicy:    true,
			wantTimeout:   true,
			wantLB:        true,
			wantBufferLim: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}
			httpRoute := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			}
			annots := annotations.NewAnnotationSet(tt.annotations)

			policy := c.generateBackendTrafficPolicy(ingress, httpRoute, annots)

			if tt.wantPolicy && policy == nil {
				t.Error("expected policy, got nil")
				return
			}
			if !tt.wantPolicy && policy != nil {
				t.Errorf("expected no policy, got %v", policy)
				return
			}
			if !tt.wantPolicy {
				return
			}

			if tt.wantTimeout && policy.Spec.ClusterSettings.Timeout == nil {
				t.Error("expected timeout config, got nil")
			}
			if tt.wantLB && policy.Spec.ClusterSettings.LoadBalancer == nil {
				t.Error("expected load balancer config, got nil")
			}
			if tt.wantBufferLim && policy.Spec.ClusterSettings.Connection == nil {
				t.Error("expected connection config, got nil")
			}
		})
	}
}

func TestBuildLoadBalancer(t *testing.T) {
	cfg := &config.Config{}
	c := New(cfg)

	tests := []struct {
		name     string
		hashBy   string
		wantType string
	}{
		{
			name:     "source IP",
			hashBy:   "$remote_addr",
			wantType: "SourceIP",
		},
		{
			name:     "binary remote addr",
			hashBy:   "$binary_remote_addr",
			wantType: "SourceIP",
		},
		{
			name:     "cookie",
			hashBy:   "$cookie_session",
			wantType: "Cookie",
		},
		{
			name:     "header",
			hashBy:   "$http_x_user_id",
			wantType: "Headers",
		},
		{
			name:     "query param",
			hashBy:   "$arg_user",
			wantType: "QueryParams",
		},
		{
			name:     "direct header name",
			hashBy:   "X-Custom-Header",
			wantType: "Headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annots := annotations.NewAnnotationSet(map[string]string{
				"nginx.ingress.kubernetes.io/upstream-hash-by": tt.hashBy,
			})

			lb := c.buildLoadBalancer(annots)
			if lb == nil {
				t.Error("expected load balancer config, got nil")
				return
			}

			if lb.ConsistentHash == nil {
				t.Error("expected consistent hash config, got nil")
				return
			}

			if string(lb.ConsistentHash.Type) != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, lb.ConsistentHash.Type)
			}
		})
	}
}

func TestGenerateClientTrafficPolicy(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "eg-gateway",
		GatewayNamespace: "envoy-gateway",
	}
	c := New(cfg)

	tests := []struct {
		name        string
		annotations map[string]string
		wantPolicy  bool
	}{
		{
			name:        "no annotations",
			annotations: map[string]string{},
			wantPolicy:  false,
		},
		{
			name: "buffer size annotation",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/proxy-buffer-size": "8k",
			},
			wantPolicy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}
			annots := annotations.NewAnnotationSet(tt.annotations)

			policy := c.generateClientTrafficPolicy(ingress, annots)

			if tt.wantPolicy && policy == nil {
				t.Error("expected policy, got nil")
				return
			}
			if !tt.wantPolicy && policy != nil {
				t.Errorf("expected no policy, got %v", policy)
			}
		})
	}
}

func TestGenerateSecurityPolicy(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "eg-gateway",
		GatewayNamespace: "envoy-gateway",
	}
	c := New(cfg)

	tests := []struct {
		name        string
		annotations map[string]string
		wantPolicy  bool
		wantCORS    bool
		wantExtAuth bool
	}{
		{
			name:        "no annotations",
			annotations: map[string]string{},
			wantPolicy:  false,
		},
		{
			name: "CORS annotations",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/enable-cors":          "true",
				"nginx.ingress.kubernetes.io/cors-allow-origin":    "https://example.com",
				"nginx.ingress.kubernetes.io/cors-allow-methods":   "GET,POST",
				"nginx.ingress.kubernetes.io/cors-allow-headers":   "Content-Type",
				"nginx.ingress.kubernetes.io/cors-max-age":         "3600",
			},
			wantPolicy: true,
			wantCORS:   true,
		},
		{
			name: "ExtAuth annotations",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/auth-url": "http://auth.default.svc.cluster.local:8080/verify",
			},
			wantPolicy:  true,
			wantExtAuth: true,
		},
		{
			name: "both CORS and ExtAuth",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/cors-allow-origin": "*",
				"nginx.ingress.kubernetes.io/auth-url":          "http://auth.default.svc.cluster.local/verify",
			},
			wantPolicy:  true,
			wantCORS:    true,
			wantExtAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}
			httpRoute := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			}
			annots := annotations.NewAnnotationSet(tt.annotations)

			policy := c.generateSecurityPolicy(context.Background(), ingress, httpRoute, annots)

			if tt.wantPolicy && policy == nil {
				t.Error("expected policy, got nil")
				return
			}
			if !tt.wantPolicy && policy != nil {
				t.Errorf("expected no policy, got %v", policy)
				return
			}
			if !tt.wantPolicy {
				return
			}

			if tt.wantCORS && policy.Spec.CORS == nil {
				t.Error("expected CORS config, got nil")
			}
			if tt.wantExtAuth && policy.Spec.ExtAuth == nil {
				t.Error("expected ExtAuth config, got nil")
			}
		})
	}
}

func TestBuildCORS(t *testing.T) {
	cfg := &config.Config{}
	c := New(cfg)

	annots := annotations.NewAnnotationSet(map[string]string{
		"nginx.ingress.kubernetes.io/cors-allow-origin":      "https://example.com, https://other.com",
		"nginx.ingress.kubernetes.io/cors-allow-methods":     "GET, POST, PUT",
		"nginx.ingress.kubernetes.io/cors-allow-headers":     "Content-Type, Authorization",
		"nginx.ingress.kubernetes.io/cors-expose-headers":    "X-Request-Id",
		"nginx.ingress.kubernetes.io/cors-max-age":           "3600",
		"nginx.ingress.kubernetes.io/cors-allow-credentials": "true",
	})

	cors := c.buildCORS(annots)

	if len(cors.AllowOrigins) != 2 {
		t.Errorf("expected 2 allow origins, got %d", len(cors.AllowOrigins))
	}
	if len(cors.AllowMethods) != 3 {
		t.Errorf("expected 3 allow methods, got %d", len(cors.AllowMethods))
	}
	if len(cors.AllowHeaders) != 2 {
		t.Errorf("expected 2 allow headers, got %d", len(cors.AllowHeaders))
	}
	if len(cors.ExposeHeaders) != 1 {
		t.Errorf("expected 1 expose header, got %d", len(cors.ExposeHeaders))
	}
	if cors.MaxAge == nil {
		t.Error("expected max age, got nil")
	}
	if cors.AllowCredentials == nil || !*cors.AllowCredentials {
		t.Error("expected allow credentials to be true")
	}
}

func TestBuildExtAuth(t *testing.T) {
	cfg := &config.Config{}
	c := New(cfg)

	tests := []struct {
		name          string
		authURL       string
		wantService   string
		wantNamespace string
		wantPort      int32
		wantPath      string
		wantNil       bool
	}{
		{
			name:          "full URL",
			authURL:       "http://auth.security.svc.cluster.local:8080/verify",
			wantService:   "auth",
			wantNamespace: "security",
			wantPort:      8080,
			wantPath:      "/verify",
		},
		{
			name:          "URL without port",
			authURL:       "http://auth.default.svc.cluster.local/verify",
			wantService:   "auth",
			wantNamespace: "default",
			wantPort:      80,
			wantPath:      "/verify",
		},
		{
			name:    "invalid URL",
			authURL: "not a url",
			wantNil: true,
		},
		{
			name:    "simple hostname",
			authURL: "http://auth/verify",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annots := annotations.NewAnnotationSet(map[string]string{
				"nginx.ingress.kubernetes.io/auth-url": tt.authURL,
			})

			extAuth := c.buildExtAuth(annots)

			if tt.wantNil {
				if extAuth != nil {
					t.Errorf("expected nil, got %v", extAuth)
				}
				return
			}

			if extAuth == nil {
				t.Error("expected extAuth config, got nil")
				return
			}

			if extAuth.HTTP == nil {
				t.Error("expected HTTP config, got nil")
				return
			}

			backendRef := extAuth.HTTP.BackendRef
			if string(backendRef.Name) != tt.wantService {
				t.Errorf("expected service %s, got %s", tt.wantService, backendRef.Name)
			}
			if backendRef.Namespace != nil && string(*backendRef.Namespace) != tt.wantNamespace {
				t.Errorf("expected namespace %s, got %s", tt.wantNamespace, *backendRef.Namespace)
			}
			if backendRef.Port == nil || int32(*backendRef.Port) != tt.wantPort {
				t.Errorf("expected port %d, got %v", tt.wantPort, backendRef.Port)
			}
			if tt.wantPath != "" && (extAuth.HTTP.Path == nil || *extAuth.HTTP.Path != tt.wantPath) {
				t.Errorf("expected path %s, got %v", tt.wantPath, extAuth.HTTP.Path)
			}
		})
	}
}

func TestBuildExtAuthWithResponseHeaders(t *testing.T) {
	cfg := &config.Config{}
	c := New(cfg)

	annots := annotations.NewAnnotationSet(map[string]string{
		"nginx.ingress.kubernetes.io/auth-url":              "http://oauth2-proxy.auth.svc.cluster.local:4180/oauth2/auth",
		"nginx.ingress.kubernetes.io/auth-response-headers": "X-Auth-Request-User, X-Auth-Request-Email, X-Auth-Request-Groups",
	})

	extAuth := c.buildExtAuth(annots)

	if extAuth == nil {
		t.Fatal("expected extAuth config, got nil")
	}

	if extAuth.HTTP == nil {
		t.Fatal("expected HTTP config, got nil")
	}

	if len(extAuth.HTTP.HeadersToBackend) != 3 {
		t.Errorf("expected 3 headers to backend, got %d", len(extAuth.HTTP.HeadersToBackend))
	}

	expectedHeaders := []string{"X-Auth-Request-User", "X-Auth-Request-Email", "X-Auth-Request-Groups"}
	for i, header := range expectedHeaders {
		if extAuth.HTTP.HeadersToBackend[i] != header {
			t.Errorf("expected header %s at index %d, got %s", header, i, extAuth.HTTP.HeadersToBackend[i])
		}
	}
}

func TestGenerateBackendTLSPolicies(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "eg-gateway",
		GatewayNamespace: "envoy-gateway",
	}
	c := New(cfg)

	tests := []struct {
		name            string
		annotations     map[string]string
		httpRoutes      []*gatewayv1.HTTPRoute
		wantPolicyCount int
		wantServices    []string
	}{
		{
			name:            "no annotation",
			annotations:     map[string]string{},
			httpRoutes:      []*gatewayv1.HTTPRoute{},
			wantPolicyCount: 0,
		},
		{
			name: "backend-protocol HTTP (not HTTPS)",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTP",
			},
			httpRoutes:      []*gatewayv1.HTTPRoute{},
			wantPolicyCount: 0,
		},
		{
			name: "backend-protocol HTTPS with one service",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
			},
			httpRoutes: []*gatewayv1.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "default"},
					Spec: gatewayv1.HTTPRouteSpec{
						Rules: []gatewayv1.HTTPRouteRule{
							{
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Kind: ptr(gatewayv1.Kind("Service")),
												Name: "my-service",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPolicyCount: 1,
			wantServices:    []string{"my-service"},
		},
		{
			name: "backend-protocol HTTPS with multiple unique services",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
			},
			httpRoutes: []*gatewayv1.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "default"},
					Spec: gatewayv1.HTTPRouteSpec{
						Rules: []gatewayv1.HTTPRouteRule{
							{
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Kind: ptr(gatewayv1.Kind("Service")),
												Name: "service-a",
											},
										},
									},
								},
							},
							{
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Kind: ptr(gatewayv1.Kind("Service")),
												Name: "service-b",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPolicyCount: 2,
			wantServices:    []string{"service-a", "service-b"},
		},
		{
			name: "backend-protocol HTTPS with duplicate services",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
			},
			httpRoutes: []*gatewayv1.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: "default"},
					Spec: gatewayv1.HTTPRouteSpec{
						Rules: []gatewayv1.HTTPRouteRule{
							{
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Kind: ptr(gatewayv1.Kind("Service")),
												Name: "my-service",
											},
										},
									},
								},
							},
							{
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Kind: ptr(gatewayv1.Kind("Service")),
												Name: "my-service",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantPolicyCount: 1,
			wantServices:    []string{"my-service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-ingress",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}
			annots := annotations.NewAnnotationSet(tt.annotations)

			policies := c.generateBackendTLSPolicies(ingress, tt.httpRoutes, annots)

			if len(policies) != tt.wantPolicyCount {
				t.Errorf("expected %d policies, got %d", tt.wantPolicyCount, len(policies))
				return
			}

			// Check that the expected services are present
			if tt.wantServices != nil {
				for i, wantService := range tt.wantServices {
					if i >= len(policies) {
						break
					}
					policy := policies[i]
					if len(policy.Spec.TargetRefs) == 0 {
						t.Error("expected at least one target ref")
						continue
					}
					if string(policy.Spec.TargetRefs[0].Name) != wantService {
						t.Errorf("expected service %s, got %s", wantService, policy.Spec.TargetRefs[0].Name)
					}
					// Check that validation uses WellKnownCACertificates: System
					if policy.Spec.Validation.WellKnownCACertificates == nil {
						t.Error("expected WellKnownCACertificates to be set")
					} else if *policy.Spec.Validation.WellKnownCACertificates != gatewayv1.WellKnownCACertificatesSystem {
						t.Errorf("expected WellKnownCACertificates System, got %s", *policy.Spec.Validation.WellKnownCACertificates)
					}
				}
			}
		})
	}
}

func TestHasBackendTLSPolicy(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "no annotation",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name: "backend-protocol HTTP",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTP",
			},
			want: false,
		},
		{
			name: "backend-protocol HTTPS",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
			},
			want: true,
		},
		{
			name: "backend-protocol lowercase https",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/backend-protocol": "https",
			},
			want: false, // Should be case-sensitive, HTTPS only
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annots := annotations.NewAnnotationSet(tt.annotations)
			got := annots.HasBackendTLSPolicy()
			if got != tt.want {
				t.Errorf("HasBackendTLSPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}
