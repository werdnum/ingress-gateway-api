package converter

import (
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ConversionResult contains all resources generated from an Ingress.
type ConversionResult struct {
	// HTTPRoutes are the generated HTTPRoute resources.
	HTTPRoutes []*gatewayv1.HTTPRoute

	// BackendTrafficPolicy is the generated BackendTrafficPolicy (if any).
	// One per HTTPRoute is created when timeout, load balancer, or body size annotations are present.
	BackendTrafficPolicies []*egv1alpha1.BackendTrafficPolicy

	// ClientTrafficPolicy is the generated ClientTrafficPolicy (if any).
	// One per Ingress is created when buffer size annotation is present.
	ClientTrafficPolicy *egv1alpha1.ClientTrafficPolicy

	// SecurityPolicy is the generated SecurityPolicy (if any).
	// One per HTTPRoute is created when CORS or ExtAuth annotations are present.
	SecurityPolicies []*egv1alpha1.SecurityPolicy

	// BackendTLSPolicies are the generated BackendTLSPolicy resources (if any).
	// One per unique backend service is created when backend-protocol: HTTPS annotation is present.
	BackendTLSPolicies []*gatewayv1.BackendTLSPolicy
}
