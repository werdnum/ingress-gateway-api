package converter

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/config"
)

func TestConvertIngress(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "gateway-ns",
	}
	conv := New(cfg)

	tests := []struct {
		name           string
		ingress        *networkingv1.Ingress
		expectedRoutes int
		checkFunc      func(t *testing.T, routes []*gatewayv1.HTTPRoute)
	}{
		{
			name: "single host with paths",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ingress",
					Namespace: "default",
					UID:       types.UID("test-uid"),
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
			},
			expectedRoutes: 1,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				route := routes[0]
				if len(route.Spec.Hostnames) != 1 || route.Spec.Hostnames[0] != "example.com" {
					t.Errorf("expected hostname example.com, got %v", route.Spec.Hostnames)
				}
				if len(route.Spec.Rules) != 1 {
					t.Errorf("expected 1 rule, got %d", len(route.Spec.Rules))
				}
				if len(route.Spec.ParentRefs) != 1 {
					t.Errorf("expected 1 parent ref, got %d", len(route.Spec.ParentRefs))
				}
				if route.Spec.ParentRefs[0].Name != "test-gateway" {
					t.Errorf("expected parent ref name test-gateway, got %s", route.Spec.ParentRefs[0].Name)
				}
			},
		},
		{
			name: "multiple hosts",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-host",
					Namespace: "default",
					UID:       types.UID("test-uid-2"),
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "api.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
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
						{
							Host: "web.example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: ptr(networkingv1.PathTypePrefix),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "web-service",
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
			},
			expectedRoutes: 2,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				hostnames := make(map[string]bool)
				for _, route := range routes {
					for _, h := range route.Spec.Hostnames {
						hostnames[string(h)] = true
					}
				}
				if !hostnames["api.example.com"] || !hostnames["web.example.com"] {
					t.Errorf("expected both hosts, got %v", hostnames)
				}
			},
		},
		{
			name: "default backend only",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-backend",
					Namespace: "default",
					UID:       types.UID("test-uid-3"),
				},
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "default-service",
							Port: networkingv1.ServiceBackendPort{
								Number: 8080,
							},
						},
					},
				},
			},
			expectedRoutes: 1,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				route := routes[0]
				if len(route.Spec.Hostnames) != 0 {
					t.Errorf("expected no hostnames for default backend, got %v", route.Spec.Hostnames)
				}
				if len(route.Spec.Rules) != 1 {
					t.Errorf("expected 1 rule, got %d", len(route.Spec.Rules))
				}
			},
		},
		{
			name: "exact path type",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exact-path",
					Namespace: "default",
					UID:       types.UID("test-uid-4"),
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/exact",
											PathType: ptr(networkingv1.PathTypeExact),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "exact-service",
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
			},
			expectedRoutes: 1,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				route := routes[0]
				if len(route.Spec.Rules) != 1 {
					t.Fatalf("expected 1 rule, got %d", len(route.Spec.Rules))
				}
				if len(route.Spec.Rules[0].Matches) != 1 {
					t.Fatalf("expected 1 match, got %d", len(route.Spec.Rules[0].Matches))
				}
				pathMatch := route.Spec.Rules[0].Matches[0].Path
				if pathMatch == nil || *pathMatch.Type != gatewayv1.PathMatchExact {
					t.Errorf("expected exact path match, got %v", pathMatch)
				}
			},
		},
		{
			name: "regex path with use-regex annotation",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "regex-ingress",
					Namespace: "default",
					UID:       types.UID("test-uid-5"),
					Annotations: map[string]string{
						"nginx.ingress.kubernetes.io/use-regex": "true",
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
											Path:     "/api/v[0-9]+/.*",
											PathType: ptr(networkingv1.PathTypeImplementationSpecific),
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
			},
			expectedRoutes: 1,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				route := routes[0]
				if len(route.Spec.Rules) != 1 {
					t.Fatalf("expected 1 rule, got %d", len(route.Spec.Rules))
				}
				if len(route.Spec.Rules[0].Matches) != 1 {
					t.Fatalf("expected 1 match, got %d", len(route.Spec.Rules[0].Matches))
				}
				pathMatch := route.Spec.Rules[0].Matches[0].Path
				if pathMatch == nil || *pathMatch.Type != gatewayv1.PathMatchRegularExpression {
					t.Errorf("expected regex path match, got %v", pathMatch)
				}
				if *pathMatch.Value != "/api/v[0-9]+/.*" {
					t.Errorf("expected path /api/v[0-9]+/.*, got %s", *pathMatch.Value)
				}
			},
		},
		{
			name: "regex path with rewrite-target capture group converts to PathPrefix",
			ingress: &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rewrite-regex-ingress",
					Namespace: "default",
					UID:       types.UID("test-uid-6"),
					Annotations: map[string]string{
						"nginx.ingress.kubernetes.io/use-regex":      "true",
						"nginx.ingress.kubernetes.io/rewrite-target": "/$2",
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
											Path:     "/data(/|$)(.*)",
											PathType: ptr(networkingv1.PathTypePrefix),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "data-service",
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
			},
			expectedRoutes: 1,
			checkFunc: func(t *testing.T, routes []*gatewayv1.HTTPRoute) {
				route := routes[0]
				if len(route.Spec.Rules) != 1 {
					t.Fatalf("expected 1 rule, got %d", len(route.Spec.Rules))
				}
				if len(route.Spec.Rules[0].Matches) != 1 {
					t.Fatalf("expected 1 match, got %d", len(route.Spec.Rules[0].Matches))
				}
				pathMatch := route.Spec.Rules[0].Matches[0].Path
				// Should be PathPrefix, not regex, because rewrite-target has capture groups
				if pathMatch == nil || *pathMatch.Type != gatewayv1.PathMatchPathPrefix {
					t.Errorf("expected PathPrefix match for rewrite with capture groups, got %v", pathMatch.Type)
				}
				// Path should be the extracted static prefix
				if *pathMatch.Value != "/data" {
					t.Errorf("expected path /data, got %s", *pathMatch.Value)
				}
				// Should have a URLRewrite filter
				if len(route.Spec.Rules[0].Filters) != 1 {
					t.Fatalf("expected 1 filter, got %d", len(route.Spec.Rules[0].Filters))
				}
				filter := route.Spec.Rules[0].Filters[0]
				if filter.Type != gatewayv1.HTTPRouteFilterURLRewrite {
					t.Errorf("expected URLRewrite filter, got %v", filter.Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := conv.ConvertIngress(context.Background(), tt.ingress)
			if len(routes) != tt.expectedRoutes {
				t.Errorf("expected %d routes, got %d", tt.expectedRoutes, len(routes))
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, routes)
			}
		})
	}
}

func TestSetOwnerReference(t *testing.T) {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ingress",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
	}

	SetOwnerReference(httpRoute, ingress)

	if len(httpRoute.OwnerReferences) != 1 {
		t.Fatalf("expected 1 owner reference, got %d", len(httpRoute.OwnerReferences))
	}

	ownerRef := httpRoute.OwnerReferences[0]
	if ownerRef.Name != "test-ingress" {
		t.Errorf("expected owner name test-ingress, got %s", ownerRef.Name)
	}
	if ownerRef.UID != "test-uid" {
		t.Errorf("expected owner UID test-uid, got %s", ownerRef.UID)
	}
	if ownerRef.Kind != "Ingress" {
		t.Errorf("expected owner kind Ingress, got %s", ownerRef.Kind)
	}
	if ownerRef.Controller == nil || !*ownerRef.Controller {
		t.Error("expected controller to be true")
	}
}

func TestGenerateRouteName(t *testing.T) {
	cfg := &config.Config{
		GatewayName:      "test-gateway",
		GatewayNamespace: "gateway-ns",
	}
	conv := New(cfg)

	tests := []struct {
		name         string
		ingressName  string
		host         string
		expectedName string
	}{
		{
			name:         "no host",
			ingressName:  "my-ingress",
			host:         "",
			expectedName: "my-ingress",
		},
		{
			name:         "simple host",
			ingressName:  "my-ingress",
			host:         "example.com",
			expectedName: "my-ingress-example-com",
		},
		{
			name:         "subdomain host",
			ingressName:  "api",
			host:         "api.example.com",
			expectedName: "api-api-example-com",
		},
		{
			name:         "wildcard host",
			ingressName:  "wildcard",
			host:         "*.example.com",
			expectedName: "wildcard-wildcard-example-com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ingress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.ingressName,
				},
			}
			result := conv.generateRouteName(ingress, tt.host)
			if result != tt.expectedName {
				t.Errorf("expected %s, got %s", tt.expectedName, result)
			}
		})
	}
}

func TestExtractStaticPrefix(t *testing.T) {
	tests := []struct {
		regexPath string
		want      string
	}{
		{"/data(/|$)(.*)", "/data"},
		{"/()(.*)", "/"},
		{"/api/v1(/|$)(.*)", "/api/v1"},
		{"/auth/realms(/|$)(.*)", "/auth/realms"},
		{"/foo/bar", "/foo/bar"},
		{"", "/"},
		{"/", "/"},
		{"/prefix(.*)", "/prefix"},
		{"/with[0-9]+regex", "/with"},
	}

	for _, tt := range tests {
		t.Run(tt.regexPath, func(t *testing.T) {
			got := extractStaticPrefix(tt.regexPath)
			if got != tt.want {
				t.Errorf("extractStaticPrefix(%q) = %q, want %q", tt.regexPath, got, tt.want)
			}
		})
	}
}
