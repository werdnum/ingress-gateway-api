package converter

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/annotations"
)

// generateBackendTrafficPolicy creates a BackendTrafficPolicy for the given HTTPRoute
// based on timeout, load balancer, and body size annotations.
func (c *Converter) generateBackendTrafficPolicy(
	ingress *networkingv1.Ingress,
	httpRoute *gatewayv1.HTTPRoute,
	annots annotations.AnnotationSet,
) *egv1alpha1.BackendTrafficPolicy {
	if !annots.HasBackendTrafficPolicyAnnotations() {
		return nil
	}

	policy := &egv1alpha1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-backend", httpRoute.Name),
			Namespace: ingress.Namespace,
			Labels:    copyLabels(ingress.Labels),
			Annotations: map[string]string{
				"ingress-gateway-api.io/source": fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
			},
		},
		Spec: egv1alpha1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRef: &gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.Group("gateway.networking.k8s.io"),
						Kind:  gatewayv1.Kind("HTTPRoute"),
						Name:  gatewayv1.ObjectName(httpRoute.Name),
					},
				},
			},
		},
	}

	// Add timeout configuration
	if annots.HasTimeout() {
		policy.Spec.ClusterSettings.Timeout = c.buildTimeout(annots)
	}

	// Add load balancer configuration
	if annots.HasLoadBalancer() {
		policy.Spec.ClusterSettings.LoadBalancer = c.buildLoadBalancer(annots)
	}

	// Add connection buffer limit from proxy-body-size
	if bodySize, ok := annots.GetQuantity(annotations.ProxyBodySize); ok {
		if policy.Spec.ClusterSettings.Connection == nil {
			policy.Spec.ClusterSettings.Connection = &egv1alpha1.BackendConnection{}
		}
		policy.Spec.ClusterSettings.Connection.BufferLimit = bodySize
	}

	return policy
}

// buildTimeout creates a Timeout configuration from annotations.
func (c *Converter) buildTimeout(annots annotations.AnnotationSet) *egv1alpha1.Timeout {
	timeout := &egv1alpha1.Timeout{
		HTTP: &egv1alpha1.HTTPTimeout{},
	}

	// Use the larger of proxy-read-timeout and proxy-send-timeout as request timeout
	var requestTimeout *gatewayv1.Duration

	if readTimeout, ok := annots.GetDuration(annotations.ProxyReadTimeout); ok {
		requestTimeout = readTimeout
	}

	if sendTimeout, ok := annots.GetDuration(annotations.ProxySendTimeout); ok {
		if requestTimeout == nil {
			requestTimeout = sendTimeout
		} else if sendTimeout != nil && *sendTimeout > *requestTimeout {
			requestTimeout = sendTimeout
		}
	}

	if requestTimeout != nil {
		timeout.HTTP.RequestTimeout = requestTimeout
	}

	return timeout
}

// buildLoadBalancer creates a LoadBalancer configuration from annotations.
func (c *Converter) buildLoadBalancer(annots annotations.AnnotationSet) *egv1alpha1.LoadBalancer {
	hashBy, ok := annots.GetString(annotations.UpstreamHashBy)
	if !ok {
		return nil
	}

	lb := &egv1alpha1.LoadBalancer{
		Type: egv1alpha1.ConsistentHashLoadBalancerType,
	}

	// Determine the hash type based on the value
	hashBy = strings.TrimSpace(hashBy)
	switch {
	case hashBy == "$remote_addr" || hashBy == "$binary_remote_addr":
		lb.ConsistentHash = &egv1alpha1.ConsistentHash{
			Type: egv1alpha1.SourceIPConsistentHashType,
		}
	case strings.HasPrefix(hashBy, "$cookie_"):
		cookieName := strings.TrimPrefix(hashBy, "$cookie_")
		lb.ConsistentHash = &egv1alpha1.ConsistentHash{
			Type: egv1alpha1.CookieConsistentHashType,
			Cookie: &egv1alpha1.Cookie{
				Name: cookieName,
			},
		}
	case strings.HasPrefix(hashBy, "$http_"):
		headerName := strings.TrimPrefix(hashBy, "$http_")
		// Convert nginx header format (underscores) to HTTP format (dashes)
		headerName = strings.ReplaceAll(headerName, "_", "-")
		lb.ConsistentHash = &egv1alpha1.ConsistentHash{
			Type:    egv1alpha1.HeadersConsistentHashType,
			Headers: []*egv1alpha1.Header{{Name: headerName}},
		}
	case strings.HasPrefix(hashBy, "$arg_"):
		paramName := strings.TrimPrefix(hashBy, "$arg_")
		lb.ConsistentHash = &egv1alpha1.ConsistentHash{
			Type:        egv1alpha1.QueryParamsConsistentHashType,
			QueryParams: []*egv1alpha1.QueryParam{{Name: paramName}},
		}
	default:
		// Treat as header name directly
		lb.ConsistentHash = &egv1alpha1.ConsistentHash{
			Type:    egv1alpha1.HeadersConsistentHashType,
			Headers: []*egv1alpha1.Header{{Name: hashBy}},
		}
	}

	return lb
}

// generateClientTrafficPolicy creates a ClientTrafficPolicy for the Ingress
// based on buffer size annotations.
func (c *Converter) generateClientTrafficPolicy(
	ingress *networkingv1.Ingress,
	annots annotations.AnnotationSet,
) *egv1alpha1.ClientTrafficPolicy {
	if !annots.HasClientTrafficPolicyAnnotations() {
		return nil
	}

	policy := &egv1alpha1.ClientTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-client", ingress.Name),
			Namespace: ingress.Namespace,
			Labels:    copyLabels(ingress.Labels),
			Annotations: map[string]string{
				"ingress-gateway-api.io/source": fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
			},
		},
		Spec: egv1alpha1.ClientTrafficPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRef: &gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.Group("gateway.networking.k8s.io"),
						Kind:  gatewayv1.Kind("Gateway"),
						Name:  gatewayv1.ObjectName(c.cfg.GatewayName),
					},
				},
			},
		},
	}

	// Add connection buffer limit from proxy-buffer-size
	if bufferSize, ok := annots.GetQuantity(annotations.ProxyBufferSize); ok {
		policy.Spec.Connection = &egv1alpha1.ClientConnection{
			BufferLimit: bufferSize,
		}
	}

	return policy
}

// generateSecurityPolicy creates a SecurityPolicy for the given HTTPRoute
// based on CORS and ExtAuth annotations.
func (c *Converter) generateSecurityPolicy(
	ctx context.Context,
	ingress *networkingv1.Ingress,
	httpRoute *gatewayv1.HTTPRoute,
	annots annotations.AnnotationSet,
) *egv1alpha1.SecurityPolicy {
	_ = ctx // Reserved for future use
	if !annots.HasSecurityPolicyAnnotations() {
		return nil
	}

	policy := &egv1alpha1.SecurityPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-security", httpRoute.Name),
			Namespace: ingress.Namespace,
			Labels:    copyLabels(ingress.Labels),
			Annotations: map[string]string{
				"ingress-gateway-api.io/source": fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
			},
		},
		Spec: egv1alpha1.SecurityPolicySpec{
			PolicyTargetReferences: egv1alpha1.PolicyTargetReferences{
				TargetRef: &gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.Group("gateway.networking.k8s.io"),
						Kind:  gatewayv1.Kind("HTTPRoute"),
						Name:  gatewayv1.ObjectName(httpRoute.Name),
					},
				},
			},
		},
	}

	// Add CORS configuration
	if annots.HasCORS() {
		policy.Spec.CORS = c.buildCORS(annots)
	}

	// Add ExtAuth configuration
	if annots.HasExtAuth() {
		policy.Spec.ExtAuth = c.buildExtAuth(annots)
	}

	return policy
}

// buildCORS creates a CORS configuration from annotations.
func (c *Converter) buildCORS(annots annotations.AnnotationSet) *egv1alpha1.CORS {
	cors := &egv1alpha1.CORS{}

	// Allow origins
	if origins, ok := annots.GetStringSlice(annotations.CORSAllowOrigin); ok {
		cors.AllowOrigins = make([]egv1alpha1.Origin, len(origins))
		for i, origin := range origins {
			cors.AllowOrigins[i] = egv1alpha1.Origin(origin)
		}
	}

	// Allow methods
	if methods, ok := annots.GetStringSlice(annotations.CORSAllowMethods); ok {
		cors.AllowMethods = methods
	}

	// Allow headers
	if headers, ok := annots.GetStringSlice(annotations.CORSAllowHeaders); ok {
		cors.AllowHeaders = headers
	}

	// Expose headers
	if headers, ok := annots.GetStringSlice(annotations.CORSExposeHeaders); ok {
		cors.ExposeHeaders = headers
	}

	// Max age
	if maxAge, ok := annots.GetDuration(annotations.CORSMaxAge); ok {
		cors.MaxAge = maxAge
	}

	// Allow credentials
	if allowCreds, ok := annots.GetBool(annotations.CORSAllowCredentials); ok {
		cors.AllowCredentials = ptr(allowCreds)
	}

	return cors
}

// buildExtAuth creates an ExtAuth configuration from annotations.
// Note: Only supports Kubernetes service URLs, not external URLs.
func (c *Converter) buildExtAuth(annots annotations.AnnotationSet) *egv1alpha1.ExtAuth {
	authURL, ok := annots.GetString(annotations.AuthURL)
	if !ok {
		return nil
	}

	// Parse the auth URL to extract service reference
	parsed, err := url.Parse(authURL)
	if err != nil {
		return nil
	}

	// Extract service name and namespace from the URL
	// Expected format: http://service.namespace.svc.cluster.local:port/path
	hostParts := strings.Split(parsed.Hostname(), ".")
	if len(hostParts) < 2 {
		return nil
	}

	serviceName := hostParts[0]
	serviceNamespace := hostParts[1]

	backendRef := &gatewayv1.BackendObjectReference{
		Group:     ptr(gatewayv1.Group("")),
		Kind:      ptr(gatewayv1.Kind("Service")),
		Name:      gatewayv1.ObjectName(serviceName),
		Namespace: ptr(gatewayv1.Namespace(serviceNamespace)),
	}

	// Set port - use explicit port if specified, otherwise default based on scheme
	var port int32
	if parsed.Port() != "" {
		p, err := parsePort(parsed.Port())
		if err == nil {
			port = p
		}
	} else {
		// Default to 80 for HTTP, 443 for HTTPS
		if parsed.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	if port > 0 {
		backendRef.Port = ptr(gatewayv1.PortNumber(port))
	}

	extAuth := &egv1alpha1.ExtAuth{
		HTTP: &egv1alpha1.HTTPExtAuthService{
			BackendCluster: egv1alpha1.BackendCluster{
				BackendRef: backendRef,
			},
		},
	}

	// Set path if specified
	if parsed.Path != "" && parsed.Path != "/" {
		extAuth.HTTP.Path = ptr(parsed.Path)
	}

	// Set headers to pass from auth response to backend
	if headers, ok := annots.GetStringSlice(annotations.AuthResponseHeaders); ok {
		extAuth.HTTP.HeadersToBackend = headers
	}

	return extAuth
}

// parsePort converts a port string to an int32.
func parsePort(port string) (int32, error) {
	var p int
	_, err := fmt.Sscanf(port, "%d", &p)
	if err != nil {
		return 0, err
	}
	return int32(p), nil
}

// SetPolicyOwnerReference sets the owner reference on a policy resource.
func SetPolicyOwnerReference(obj metav1.Object, ingress *networkingv1.Ingress) {
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         "networking.k8s.io/v1",
			Kind:               "Ingress",
			Name:               ingress.Name,
			UID:                ingress.UID,
			Controller:         ptr(true),
			BlockOwnerDeletion: ptr(true),
		},
	})
}
