package converter

import (
	"context"
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/annotations"
	"github.com/werdnum/ingress-gateway-api/internal/config"
)

// Converter converts Ingress resources to HTTPRoutes.
type Converter struct {
	cfg      *config.Config
	resolver ServicePortResolver
}

// New creates a new Converter.
func New(cfg *config.Config) *Converter {
	return &Converter{
		cfg:      cfg,
		resolver: &NoopServicePortResolver{},
	}
}

// NewWithResolver creates a new Converter with a service port resolver.
func NewWithResolver(cfg *config.Config, resolver ServicePortResolver) *Converter {
	return &Converter{
		cfg:      cfg,
		resolver: resolver,
	}
}

// ConvertIngress converts an Ingress resource to HTTPRoute(s).
// It creates one HTTPRoute per host in the Ingress.
// For backward compatibility, this method does not generate policies.
// Use ConvertIngressFull for complete conversion including policies.
func (c *Converter) ConvertIngress(ctx context.Context, ingress *networkingv1.Ingress) []*gatewayv1.HTTPRoute {
	result := c.ConvertIngressFull(ctx, ingress)
	return result.HTTPRoutes
}

// ConvertIngressFull converts an Ingress resource to HTTPRoute(s) and associated policies.
// It creates one HTTPRoute per host in the Ingress, along with:
// - BackendTrafficPolicy for timeout, load balancer, and body size annotations
// - ClientTrafficPolicy for buffer size annotation
// - SecurityPolicy for CORS and ExtAuth annotations
func (c *Converter) ConvertIngressFull(ctx context.Context, ingress *networkingv1.Ingress) *ConversionResult {
	result := &ConversionResult{}
	annots := annotations.NewAnnotationSet(ingress.Annotations)

	// Group rules by host
	rulesByHost := make(map[string][]networkingv1.HTTPIngressPath)
	for _, rule := range ingress.Spec.Rules {
		host := rule.Host
		if rule.HTTP != nil {
			rulesByHost[host] = append(rulesByHost[host], rule.HTTP.Paths...)
		}
	}

	// Create an HTTPRoute for each host
	for host, paths := range rulesByHost {
		httpRoute := c.createHTTPRouteWithFilters(ctx, ingress, host, paths, annots)
		result.HTTPRoutes = append(result.HTTPRoutes, httpRoute)

		// Generate BackendTrafficPolicy if needed
		if btp := c.generateBackendTrafficPolicy(ingress, httpRoute, annots); btp != nil {
			result.BackendTrafficPolicies = append(result.BackendTrafficPolicies, btp)
		}

		// Generate SecurityPolicy if needed
		if sp := c.generateSecurityPolicy(ctx, ingress, httpRoute, annots); sp != nil {
			result.SecurityPolicies = append(result.SecurityPolicies, sp)
		}
	}

	// Handle default backend if present and no other rules
	if ingress.Spec.DefaultBackend != nil && len(result.HTTPRoutes) == 0 {
		httpRoute := c.createDefaultBackendRoute(ctx, ingress)
		result.HTTPRoutes = append(result.HTTPRoutes, httpRoute)

		// Generate BackendTrafficPolicy if needed
		if btp := c.generateBackendTrafficPolicy(ingress, httpRoute, annots); btp != nil {
			result.BackendTrafficPolicies = append(result.BackendTrafficPolicies, btp)
		}

		// Generate SecurityPolicy if needed
		if sp := c.generateSecurityPolicy(ctx, ingress, httpRoute, annots); sp != nil {
			result.SecurityPolicies = append(result.SecurityPolicies, sp)
		}
	}

	// Generate ClientTrafficPolicy (one per Ingress, targets Gateway)
	if ctp := c.generateClientTrafficPolicy(ingress, annots); ctp != nil {
		result.ClientTrafficPolicy = ctp
	}

	return result
}

// createHTTPRoute creates an HTTPRoute for a specific host.
// This is the legacy method that doesn't apply filters.
func (c *Converter) createHTTPRoute(ctx context.Context, ingress *networkingv1.Ingress, host string, paths []networkingv1.HTTPIngressPath) *gatewayv1.HTTPRoute {
	return c.createHTTPRouteWithFilters(ctx, ingress, host, paths, nil)
}

// createHTTPRouteWithFilters creates an HTTPRoute for a specific host with optional filter support.
func (c *Converter) createHTTPRouteWithFilters(ctx context.Context, ingress *networkingv1.Ingress, host string, paths []networkingv1.HTTPIngressPath, annots annotations.AnnotationSet) *gatewayv1.HTTPRoute {
	routeName := c.generateRouteName(ingress, host)

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: ingress.Namespace,
			Labels:    copyLabels(ingress.Labels),
			Annotations: map[string]string{
				"ingress-gateway-api.io/source": fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					c.createParentRef(),
				},
			},
		},
	}

	// Set hostnames
	if host != "" {
		httpRoute.Spec.Hostnames = []gatewayv1.Hostname{gatewayv1.Hostname(host)}
	}

	// Convert paths to rules with filters
	for _, path := range paths {
		rule := c.convertPathWithFilters(ctx, ingress.Namespace, path, annots)
		httpRoute.Spec.Rules = append(httpRoute.Spec.Rules, rule)
	}

	return httpRoute
}

// createDefaultBackendRoute creates an HTTPRoute for the default backend.
func (c *Converter) createDefaultBackendRoute(ctx context.Context, ingress *networkingv1.Ingress) *gatewayv1.HTTPRoute {
	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingress.Name,
			Namespace: ingress.Namespace,
			Labels:    copyLabels(ingress.Labels),
			Annotations: map[string]string{
				"ingress-gateway-api.io/source": fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					c.createParentRef(),
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						c.convertBackend(ctx, ingress.Namespace, ingress.Spec.DefaultBackend),
					},
				},
			},
		},
	}

	return httpRoute
}

// convertPath converts an Ingress path to an HTTPRouteRule.
// This is the legacy method that doesn't apply filters.
func (c *Converter) convertPath(ctx context.Context, namespace string, path networkingv1.HTTPIngressPath) gatewayv1.HTTPRouteRule {
	return c.convertPathWithFilters(ctx, namespace, path, nil)
}

// convertPathWithFilters converts an Ingress path to an HTTPRouteRule with optional filter support.
func (c *Converter) convertPathWithFilters(ctx context.Context, namespace string, path networkingv1.HTTPIngressPath, annots annotations.AnnotationSet) gatewayv1.HTTPRouteRule {
	rule := gatewayv1.HTTPRouteRule{}

	// Convert path match
	originalPath := path.Path
	if originalPath != "" {
		pathMatch := c.convertPathMatch(path, annots)
		rule.Matches = []gatewayv1.HTTPRouteMatch{
			{
				Path: &pathMatch,
			},
		}
	}

	// Apply filters from annotations
	hasRedirect := false
	if annots != nil && annots.HasHTTPRouteFilters() {
		hasRedirect = applyFilters(&rule, annots, originalPath)
	}

	// Convert backend (skip if redirect filter is applied)
	if !hasRedirect {
		rule.BackendRefs = []gatewayv1.HTTPBackendRef{
			c.convertIngressBackend(ctx, namespace, path.Backend),
		}
	}

	return rule
}

// convertPathMatch converts an Ingress path type to Gateway API path match.
func (c *Converter) convertPathMatch(path networkingv1.HTTPIngressPath, annots annotations.AnnotationSet) gatewayv1.HTTPPathMatch {
	pathMatch := gatewayv1.HTTPPathMatch{
		Value: ptr(path.Path),
	}

	pathType := networkingv1.PathTypePrefix
	if path.PathType != nil {
		pathType = *path.PathType
	}

	// Check if regex is enabled via annotation
	useRegex := false
	if annots != nil {
		if v, ok := annots.GetBool(annotations.UseRegex); ok && v {
			useRegex = true
		}
	}

	switch pathType {
	case networkingv1.PathTypeExact:
		pathMatch.Type = ptr(gatewayv1.PathMatchExact)
	case networkingv1.PathTypePrefix:
		if useRegex {
			pathMatch.Type = ptr(gatewayv1.PathMatchRegularExpression)
		} else {
			pathMatch.Type = ptr(gatewayv1.PathMatchPathPrefix)
		}
	case networkingv1.PathTypeImplementationSpecific:
		// For implementation-specific, check if regex is enabled
		if useRegex {
			pathMatch.Type = ptr(gatewayv1.PathMatchRegularExpression)
		} else {
			pathMatch.Type = ptr(gatewayv1.PathMatchPathPrefix)
		}
	}

	return pathMatch
}

// convertIngressBackend converts an Ingress backend to an HTTPBackendRef.
func (c *Converter) convertIngressBackend(ctx context.Context, namespace string, backend networkingv1.IngressBackend) gatewayv1.HTTPBackendRef {
	if backend.Service != nil {
		port := c.resolveServicePort(ctx, namespace, backend.Service.Name, backend.Service.Port)
		return gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: ptr(gatewayv1.Group("")),
					Kind:  ptr(gatewayv1.Kind("Service")),
					Name:  gatewayv1.ObjectName(backend.Service.Name),
					Port:  port,
				},
			},
		}
	}

	// Resource backend (not common, but handle it)
	if backend.Resource != nil {
		var group gatewayv1.Group
		if backend.Resource.APIGroup != nil {
			group = gatewayv1.Group(*backend.Resource.APIGroup)
		}
		return gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: ptr(group),
					Kind:  ptr(gatewayv1.Kind(backend.Resource.Kind)),
					Name:  gatewayv1.ObjectName(backend.Resource.Name),
				},
			},
		}
	}

	return gatewayv1.HTTPBackendRef{}
}

// convertBackend converts an IngressBackend to an HTTPBackendRef.
func (c *Converter) convertBackend(ctx context.Context, namespace string, backend *networkingv1.IngressBackend) gatewayv1.HTTPBackendRef {
	if backend == nil {
		return gatewayv1.HTTPBackendRef{}
	}
	return c.convertIngressBackend(ctx, namespace, *backend)
}

// resolveServicePort resolves a service port (named or numeric) to a Gateway API port number.
func (c *Converter) resolveServicePort(ctx context.Context, namespace, serviceName string, port networkingv1.ServiceBackendPort) *gatewayv1.PortNumber {
	resolved, err := c.resolver.ResolvePort(ctx, namespace, serviceName, port.Name, port.Number)
	if err != nil {
		// Log error but continue - the HTTPRoute will fail validation
		// which is better than silently dropping the backend
		return nil
	}
	return ptr(gatewayv1.PortNumber(resolved))
}

// createParentRef creates a ParentReference to the shared Gateway.
func (c *Converter) createParentRef() gatewayv1.ParentReference {
	return gatewayv1.ParentReference{
		Group:     ptr(gatewayv1.Group("gateway.networking.k8s.io")),
		Kind:      ptr(gatewayv1.Kind("Gateway")),
		Namespace: ptr(gatewayv1.Namespace(c.cfg.GatewayNamespace)),
		Name:      gatewayv1.ObjectName(c.cfg.GatewayName),
	}
}

// generateRouteName generates a unique name for the HTTPRoute.
func (c *Converter) generateRouteName(ingress *networkingv1.Ingress, host string) string {
	if host == "" {
		return ingress.Name
	}
	// Sanitize host for use in resource name
	sanitized := strings.ReplaceAll(host, ".", "-")
	sanitized = strings.ReplaceAll(sanitized, "*", "wildcard")
	return fmt.Sprintf("%s-%s", ingress.Name, sanitized)
}

// copyLabels creates a copy of labels map.
func copyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return nil
	}
	copy := make(map[string]string, len(labels))
	for k, v := range labels {
		copy[k] = v
	}
	return copy
}

// SetOwnerReference sets the owner reference on the HTTPRoute.
func SetOwnerReference(httpRoute *gatewayv1.HTTPRoute, ingress *networkingv1.Ingress) {
	httpRoute.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         "networking.k8s.io/v1",
			Kind:               "Ingress",
			Name:               ingress.Name,
			UID:                ingress.UID,
			Controller:         ptr(true),
			BlockOwnerDeletion: ptr(true),
		},
	}
}

func ptr[T any](v T) *T {
	return &v
}
