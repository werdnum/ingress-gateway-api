package converter

import (
	"regexp"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/werdnum/ingress-gateway-api/internal/annotations"
)

// addRewriteFilter adds a URLRewrite filter to the rule based on rewrite-target annotation.
// Handles nginx capture group syntax (e.g., /$1, /$2) by converting to Gateway API format.
func addRewriteFilter(rule *gatewayv1.HTTPRouteRule, annots annotations.AnnotationSet, originalPath string) {
	rewriteTarget, ok := annots.GetString(annotations.RewriteTarget)
	if !ok {
		return
	}

	filter := gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterURLRewrite,
		URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
			Path: &gatewayv1.HTTPPathModifier{},
		},
	}

	// Check if the rewrite target contains capture groups
	if containsCaptureGroups(rewriteTarget) {
		// Use ReplacePrefixMatch for capture group replacement
		// This is a simplified approach - full regex support would require ExtensionRef
		filter.URLRewrite.Path.Type = gatewayv1.PrefixMatchHTTPPathModifier
		filter.URLRewrite.Path.ReplacePrefixMatch = ptr(convertCaptureGroups(rewriteTarget, originalPath))
	} else {
		// Simple static replacement
		if rewriteTarget == "/" {
			filter.URLRewrite.Path.Type = gatewayv1.PrefixMatchHTTPPathModifier
			filter.URLRewrite.Path.ReplacePrefixMatch = ptr("/")
		} else {
			filter.URLRewrite.Path.Type = gatewayv1.FullPathHTTPPathModifier
			filter.URLRewrite.Path.ReplaceFullPath = ptr(rewriteTarget)
		}
	}

	rule.Filters = append(rule.Filters, filter)
}

// containsCaptureGroups checks if the rewrite target contains nginx capture group references.
func containsCaptureGroups(target string) bool {
	// Check for $1, $2, etc.
	return regexp.MustCompile(`\$\d+`).MatchString(target)
}

// convertCaptureGroups converts nginx capture group syntax to a static path.
// This is a best-effort conversion as Gateway API doesn't support regex capture groups.
// For complex patterns, users should use ExtensionRef with a custom filter.
func convertCaptureGroups(target string, originalPath string) string {
	// If the target is just $1 or /$1, use prefix replacement
	if target == "/$1" || target == "$1" {
		// Return empty string for prefix replacement - the matched prefix will be removed
		return "/"
	}

	// For more complex patterns, strip the capture group references
	// and return what remains
	result := regexp.MustCompile(`\$\d+`).ReplaceAllString(target, "")

	// Clean up any double slashes
	result = strings.ReplaceAll(result, "//", "/")

	if result == "" {
		result = "/"
	}

	return result
}

// addAppRootRedirect adds a RequestRedirect filter for app-root annotation.
// This redirects requests to "/" to the specified app root.
func addAppRootRedirect(rule *gatewayv1.HTTPRouteRule, annots annotations.AnnotationSet) {
	appRoot, ok := annots.GetString(annotations.AppRoot)
	if !ok {
		return
	}

	// Only apply to root path
	if len(rule.Matches) == 0 {
		return
	}

	for _, match := range rule.Matches {
		if match.Path != nil && match.Path.Value != nil && *match.Path.Value == "/" {
			filter := gatewayv1.HTTPRouteFilter{
				Type: gatewayv1.HTTPRouteFilterRequestRedirect,
				RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
					Path: &gatewayv1.HTTPPathModifier{
						Type:            gatewayv1.FullPathHTTPPathModifier,
						ReplaceFullPath: ptr(appRoot),
					},
					StatusCode: ptr(302),
				},
			}
			rule.Filters = append(rule.Filters, filter)
			return
		}
	}
}

// addSSLRedirectFilter adds a RequestRedirect filter for HTTP to HTTPS redirect.
func addSSLRedirectFilter(rule *gatewayv1.HTTPRouteRule, annots annotations.AnnotationSet) {
	if !annots.HasSSLRedirect() {
		return
	}

	filter := gatewayv1.HTTPRouteFilter{
		Type: gatewayv1.HTTPRouteFilterRequestRedirect,
		RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
			Scheme:     ptr("https"),
			StatusCode: ptr(301),
		},
	}

	rule.Filters = append(rule.Filters, filter)
}

// applyFilters applies all annotation-based filters to an HTTPRoute rule.
// Returns true if any redirect filter was applied (meaning backend refs should be removed).
func applyFilters(rule *gatewayv1.HTTPRouteRule, annots annotations.AnnotationSet, originalPath string) bool {
	hasRedirect := false

	// Check for SSL redirect first - if enabled, no other filters or backends needed
	if annots.HasSSLRedirect() {
		addSSLRedirectFilter(rule, annots)
		hasRedirect = true
	}

	// Check for app root redirect on root path
	if annots.HasAppRoot() {
		beforeLen := len(rule.Filters)
		addAppRootRedirect(rule, annots)
		if len(rule.Filters) > beforeLen {
			hasRedirect = true
		}
	}

	// Add rewrite filter if not a redirect
	if !hasRedirect && annots.HasRewrite() {
		addRewriteFilter(rule, annots, originalPath)
	}

	return hasRedirect
}
