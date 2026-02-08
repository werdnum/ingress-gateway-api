package converter

import (
	"testing"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/annotations"
)

func TestAddRewriteFilter(t *testing.T) {
	tests := []struct {
		name           string
		rewriteTarget  string
		originalPath   string
		wantFilterType gatewayv1.HTTPRouteFilterType
		wantPathType   gatewayv1.HTTPPathModifierType
	}{
		{
			name:           "simple rewrite to root",
			rewriteTarget:  "/",
			originalPath:   "/api",
			wantFilterType: gatewayv1.HTTPRouteFilterURLRewrite,
			wantPathType:   gatewayv1.PrefixMatchHTTPPathModifier,
		},
		{
			name:           "rewrite to specific path",
			rewriteTarget:  "/v2",
			originalPath:   "/api",
			wantFilterType: gatewayv1.HTTPRouteFilterURLRewrite,
			wantPathType:   gatewayv1.FullPathHTTPPathModifier,
		},
		{
			name:           "capture group rewrite",
			rewriteTarget:  "/$1",
			originalPath:   "/api/(.*)",
			wantFilterType: gatewayv1.HTTPRouteFilterURLRewrite,
			wantPathType:   gatewayv1.PrefixMatchHTTPPathModifier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &gatewayv1.HTTPRouteRule{}
			annots := annotations.NewAnnotationSet(map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": tt.rewriteTarget,
			})

			addRewriteFilter(rule, annots, tt.originalPath)

			if len(rule.Filters) != 1 {
				t.Errorf("expected 1 filter, got %d", len(rule.Filters))
				return
			}

			filter := rule.Filters[0]
			if filter.Type != tt.wantFilterType {
				t.Errorf("expected filter type %s, got %s", tt.wantFilterType, filter.Type)
			}

			if filter.URLRewrite == nil || filter.URLRewrite.Path == nil {
				t.Error("expected URL rewrite path config")
				return
			}

			if filter.URLRewrite.Path.Type != tt.wantPathType {
				t.Errorf("expected path type %s, got %s", tt.wantPathType, filter.URLRewrite.Path.Type)
			}
		})
	}
}

func TestAddAppRootRedirect(t *testing.T) {
	tests := []struct {
		name       string
		appRoot    string
		pathValue  string
		wantFilter bool
	}{
		{
			name:       "root path gets redirect",
			appRoot:    "/app",
			pathValue:  "/",
			wantFilter: true,
		},
		{
			name:       "non-root path no redirect",
			appRoot:    "/app",
			pathValue:  "/api",
			wantFilter: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Value: ptr(tt.pathValue),
						},
					},
				},
			}
			annots := annotations.NewAnnotationSet(map[string]string{
				"nginx.ingress.kubernetes.io/app-root": tt.appRoot,
			})

			addAppRootRedirect(rule, annots)

			if tt.wantFilter && len(rule.Filters) == 0 {
				t.Error("expected filter to be added")
			}
			if !tt.wantFilter && len(rule.Filters) > 0 {
				t.Error("expected no filter to be added")
			}

			if tt.wantFilter && len(rule.Filters) > 0 {
				filter := rule.Filters[0]
				if filter.Type != gatewayv1.HTTPRouteFilterRequestRedirect {
					t.Errorf("expected RequestRedirect filter, got %s", filter.Type)
				}
				if filter.RequestRedirect == nil || filter.RequestRedirect.Path == nil {
					t.Error("expected redirect path config")
					return
				}
				if filter.RequestRedirect.Path.ReplaceFullPath == nil ||
					*filter.RequestRedirect.Path.ReplaceFullPath != tt.appRoot {
					t.Errorf("expected redirect to %s", tt.appRoot)
				}
			}
		})
	}
}

func TestAddSSLRedirectFilter(t *testing.T) {
	tests := []struct {
		name        string
		sslRedirect string
		wantFilter  bool
	}{
		{
			name:        "ssl redirect true",
			sslRedirect: "true",
			wantFilter:  true,
		},
		{
			name:        "ssl redirect false",
			sslRedirect: "false",
			wantFilter:  false,
		},
		{
			name:        "no ssl redirect annotation",
			sslRedirect: "",
			wantFilter:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &gatewayv1.HTTPRouteRule{}
			annots := map[string]string{}
			if tt.sslRedirect != "" {
				annots["nginx.ingress.kubernetes.io/ssl-redirect"] = tt.sslRedirect
			}
			as := annotations.NewAnnotationSet(annots)

			addSSLRedirectFilter(rule, as)

			if tt.wantFilter && len(rule.Filters) == 0 {
				t.Error("expected filter to be added")
			}
			if !tt.wantFilter && len(rule.Filters) > 0 {
				t.Error("expected no filter to be added")
			}

			if tt.wantFilter && len(rule.Filters) > 0 {
				filter := rule.Filters[0]
				if filter.Type != gatewayv1.HTTPRouteFilterRequestRedirect {
					t.Errorf("expected RequestRedirect filter, got %s", filter.Type)
				}
				if filter.RequestRedirect == nil {
					t.Error("expected redirect config")
					return
				}
				if filter.RequestRedirect.Scheme == nil || *filter.RequestRedirect.Scheme != "https" {
					t.Error("expected scheme to be https")
				}
				if filter.RequestRedirect.StatusCode == nil || *filter.RequestRedirect.StatusCode != 301 {
					t.Error("expected status code 301")
				}
			}
		})
	}
}

func TestApplyFilters(t *testing.T) {
	tests := []struct {
		name         string
		annotations  map[string]string
		wantFilters  int
		wantRedirect bool
	}{
		{
			name:         "no filter annotations",
			annotations:  map[string]string{},
			wantFilters:  0,
			wantRedirect: false,
		},
		{
			name: "ssl redirect takes priority",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/ssl-redirect":   "true",
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
			wantFilters:  1,
			wantRedirect: true,
		},
		{
			name: "rewrite only",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/api",
			},
			wantFilters:  1,
			wantRedirect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{
					{
						Path: &gatewayv1.HTTPPathMatch{
							Value: ptr("/test"),
						},
					},
				},
			}
			annots := annotations.NewAnnotationSet(tt.annotations)

			hasRedirect := applyFilters(rule, annots, "/test")

			if hasRedirect != tt.wantRedirect {
				t.Errorf("expected hasRedirect = %v, got %v", tt.wantRedirect, hasRedirect)
			}
			if len(rule.Filters) != tt.wantFilters {
				t.Errorf("expected %d filters, got %d", tt.wantFilters, len(rule.Filters))
			}
		})
	}
}

func TestContainsCaptureGroups(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"/$1", true},
		{"/api/$2/path", true},
		{"/static/path", false},
		{"/", false},
		{"$1", true},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := containsCaptureGroups(tt.target); got != tt.want {
				t.Errorf("containsCaptureGroups(%s) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestConvertCaptureGroups(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"/$1", "/"},
		{"$1", "/"},
		{"/api/$1/path", "/api/path"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := convertCaptureGroups(tt.target, "/original"); got != tt.want {
				t.Errorf("convertCaptureGroups(%s) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}
