package converter

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/config"
)

// Converter converts Ingress resources to HTTPRoutes.
type Converter struct {
	cfg *config.Config
}

// New creates a new Converter.
func New(cfg *config.Config) *Converter {
	return &Converter{cfg: cfg}
}

// ConvertIngress converts an Ingress resource to HTTPRoute(s).
// It creates one HTTPRoute per host in the Ingress.
func (c *Converter) ConvertIngress(ingress *networkingv1.Ingress) []*gatewayv1.HTTPRoute {
	var httpRoutes []*gatewayv1.HTTPRoute

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
		httpRoute := c.createHTTPRoute(ingress, host, paths)
		httpRoutes = append(httpRoutes, httpRoute)
	}

	// Handle default backend if present and no other rules
	if ingress.Spec.DefaultBackend != nil && len(httpRoutes) == 0 {
		httpRoute := c.createDefaultBackendRoute(ingress)
		httpRoutes = append(httpRoutes, httpRoute)
	}

	return httpRoutes
}

// createHTTPRoute creates an HTTPRoute for a specific host.
func (c *Converter) createHTTPRoute(ingress *networkingv1.Ingress, host string, paths []networkingv1.HTTPIngressPath) *gatewayv1.HTTPRoute {
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

	// Convert paths to rules
	for _, path := range paths {
		rule := c.convertPath(ingress.Namespace, path)
		httpRoute.Spec.Rules = append(httpRoute.Spec.Rules, rule)
	}

	return httpRoute
}

// createDefaultBackendRoute creates an HTTPRoute for the default backend.
func (c *Converter) createDefaultBackendRoute(ingress *networkingv1.Ingress) *gatewayv1.HTTPRoute {
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
						c.convertBackend(ingress.Namespace, ingress.Spec.DefaultBackend),
					},
				},
			},
		},
	}

	return httpRoute
}

// convertPath converts an Ingress path to an HTTPRouteRule.
func (c *Converter) convertPath(namespace string, path networkingv1.HTTPIngressPath) gatewayv1.HTTPRouteRule {
	rule := gatewayv1.HTTPRouteRule{}

	// Convert path match
	if path.Path != "" {
		pathMatch := c.convertPathMatch(path)
		rule.Matches = []gatewayv1.HTTPRouteMatch{
			{
				Path: &pathMatch,
			},
		}
	}

	// Convert backend
	rule.BackendRefs = []gatewayv1.HTTPBackendRef{
		c.convertIngressBackend(namespace, path.Backend),
	}

	return rule
}

// convertPathMatch converts an Ingress path type to Gateway API path match.
func (c *Converter) convertPathMatch(path networkingv1.HTTPIngressPath) gatewayv1.HTTPPathMatch {
	pathMatch := gatewayv1.HTTPPathMatch{
		Value: ptr(path.Path),
	}

	pathType := networkingv1.PathTypePrefix
	if path.PathType != nil {
		pathType = *path.PathType
	}

	switch pathType {
	case networkingv1.PathTypeExact:
		pathMatch.Type = ptr(gatewayv1.PathMatchExact)
	case networkingv1.PathTypePrefix:
		pathMatch.Type = ptr(gatewayv1.PathMatchPathPrefix)
	case networkingv1.PathTypeImplementationSpecific:
		// Default to prefix for implementation-specific
		pathMatch.Type = ptr(gatewayv1.PathMatchPathPrefix)
	}

	return pathMatch
}

// convertIngressBackend converts an Ingress backend to an HTTPBackendRef.
func (c *Converter) convertIngressBackend(namespace string, backend networkingv1.IngressBackend) gatewayv1.HTTPBackendRef {
	if backend.Service != nil {
		return gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: ptr(gatewayv1.Group("")),
					Kind:  ptr(gatewayv1.Kind("Service")),
					Name:  gatewayv1.ObjectName(backend.Service.Name),
					Port:  c.convertServicePort(backend.Service.Port),
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
func (c *Converter) convertBackend(namespace string, backend *networkingv1.IngressBackend) gatewayv1.HTTPBackendRef {
	if backend == nil {
		return gatewayv1.HTTPBackendRef{}
	}
	return c.convertIngressBackend(namespace, *backend)
}

// convertServicePort converts an Ingress service port to a Gateway API port.
func (c *Converter) convertServicePort(port networkingv1.ServiceBackendPort) *gatewayv1.PortNumber {
	if port.Number != 0 {
		return ptr(gatewayv1.PortNumber(port.Number))
	}
	// Named ports are not directly supported in Gateway API BackendRef
	// The service will need to be resolved by the gateway implementation
	return nil
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
