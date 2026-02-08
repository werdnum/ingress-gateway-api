package annotations

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// AnnotationSet provides typed access to Ingress annotations.
type AnnotationSet map[string]string

// NewAnnotationSet creates a new AnnotationSet from a map.
func NewAnnotationSet(annotations map[string]string) AnnotationSet {
	if annotations == nil {
		return make(AnnotationSet)
	}
	return AnnotationSet(annotations)
}

// GetString returns the string value of an annotation.
func (a AnnotationSet) GetString(key string) (string, bool) {
	val, ok := a[key]
	return val, ok
}

// GetDuration parses an annotation value as a duration.
// Returns the duration as a Gateway API Duration string and whether it was successfully parsed.
func (a AnnotationSet) GetDuration(key string) (*gatewayv1.Duration, bool) {
	val, ok := a[key]
	if !ok {
		return nil, false
	}

	// Try parsing as integer seconds first
	if seconds, err := strconv.Atoi(val); err == nil {
		d := gatewayv1.Duration(formatDuration(time.Duration(seconds) * time.Second))
		return &d, true
	}

	// Try parsing as Go duration string
	if dur, err := time.ParseDuration(val); err == nil {
		d := gatewayv1.Duration(formatDuration(dur))
		return &d, true
	}

	return nil, false
}

// formatDuration formats a time.Duration as a Gateway API Duration string.
// Gateway API Duration format: ^([0-9]{1,5}(h|m|s|ms)){1,4}$
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	var result string

	hours := d / time.Hour
	if hours > 0 {
		result += strconv.FormatInt(int64(hours), 10) + "h"
		d -= hours * time.Hour
	}

	minutes := d / time.Minute
	if minutes > 0 {
		result += strconv.FormatInt(int64(minutes), 10) + "m"
		d -= minutes * time.Minute
	}

	seconds := d / time.Second
	if seconds > 0 {
		result += strconv.FormatInt(int64(seconds), 10) + "s"
		d -= seconds * time.Second
	}

	ms := d / time.Millisecond
	if ms > 0 {
		result += strconv.FormatInt(int64(ms), 10) + "ms"
	}

	if result == "" {
		return "0s"
	}

	return result
}

// GetMetav1Duration parses an annotation value as a metav1.Duration.
func (a AnnotationSet) GetMetav1Duration(key string) (*metav1.Duration, bool) {
	val, ok := a[key]
	if !ok {
		return nil, false
	}

	// Try parsing as integer seconds first
	if seconds, err := strconv.Atoi(val); err == nil {
		return &metav1.Duration{Duration: time.Duration(seconds) * time.Second}, true
	}

	// Try parsing as Go duration string
	if dur, err := time.ParseDuration(val); err == nil {
		return &metav1.Duration{Duration: dur}, true
	}

	return nil, false
}

// GetQuantity parses an annotation value as a resource.Quantity.
// Supports nginx format like "4k", "16m", etc.
func (a AnnotationSet) GetQuantity(key string) (*resource.Quantity, bool) {
	val, ok := a[key]
	if !ok {
		return nil, false
	}

	// Normalize nginx size format to Kubernetes format
	// nginx uses lowercase (4k, 16m), Kubernetes uses uppercase (4Ki, 16Mi)
	normalized := normalizeSize(val)

	q, err := resource.ParseQuantity(normalized)
	if err != nil {
		return nil, false
	}

	return &q, true
}

// normalizeSize converts nginx size format to Kubernetes format.
func normalizeSize(val string) string {
	val = strings.TrimSpace(val)
	if len(val) == 0 {
		return val
	}

	// Check for nginx suffixes and convert
	lastChar := val[len(val)-1]
	switch lastChar {
	case 'k':
		return val[:len(val)-1] + "Ki"
	case 'm':
		return val[:len(val)-1] + "Mi"
	case 'g':
		return val[:len(val)-1] + "Gi"
	case 'K', 'M', 'G':
		// Already uppercase, might need 'i' suffix
		if !strings.HasSuffix(val, "i") {
			return val + "i"
		}
	}

	return val
}

// GetBool parses an annotation value as a boolean.
func (a AnnotationSet) GetBool(key string) (bool, bool) {
	val, ok := a[key]
	if !ok {
		return false, false
	}

	b, err := strconv.ParseBool(val)
	if err != nil {
		return false, false
	}

	return b, true
}

// GetStringSlice parses an annotation value as a comma-separated list.
func (a AnnotationSet) GetStringSlice(key string) ([]string, bool) {
	val, ok := a[key]
	if !ok {
		return nil, false
	}

	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return nil, false
	}

	return result, true
}

// HasTimeout returns true if any timeout annotation is present.
func (a AnnotationSet) HasTimeout() bool {
	_, hasRead := a[ProxyReadTimeout]
	_, hasSend := a[ProxySendTimeout]
	return hasRead || hasSend
}

// HasBufferSize returns true if any buffer size annotation is present.
func (a AnnotationSet) HasBufferSize() bool {
	_, hasBuffer := a[ProxyBufferSize]
	_, hasBody := a[ProxyBodySize]
	return hasBuffer || hasBody
}

// HasLoadBalancer returns true if load balancer annotation is present.
func (a AnnotationSet) HasLoadBalancer() bool {
	_, ok := a[UpstreamHashBy]
	return ok
}

// HasCORS returns true if any CORS annotation is present.
func (a AnnotationSet) HasCORS() bool {
	// Check for explicit enable
	if enabled, ok := a.GetBool(CORSEnabled); ok && enabled {
		return true
	}

	// Check for any CORS configuration annotation
	corsAnnotations := []string{
		CORSAllowOrigin,
		CORSAllowMethods,
		CORSAllowHeaders,
		CORSExposeHeaders,
		CORSMaxAge,
		CORSAllowCredentials,
	}

	for _, ann := range corsAnnotations {
		if _, ok := a[ann]; ok {
			return true
		}
	}

	return false
}

// HasExtAuth returns true if external auth annotations are present.
func (a AnnotationSet) HasExtAuth() bool {
	_, ok := a[AuthURL]
	return ok
}

// HasRewrite returns true if rewrite-target annotation is present.
func (a AnnotationSet) HasRewrite() bool {
	_, ok := a[RewriteTarget]
	return ok
}

// HasAppRoot returns true if app-root annotation is present.
func (a AnnotationSet) HasAppRoot() bool {
	_, ok := a[AppRoot]
	return ok
}

// HasSSLRedirect returns true if ssl-redirect annotation is present and true.
func (a AnnotationSet) HasSSLRedirect() bool {
	if val, ok := a.GetBool(SSLRedirect); ok && val {
		return true
	}
	return false
}

// HasBackendTrafficPolicyAnnotations returns true if any BackendTrafficPolicy annotation is present.
func (a AnnotationSet) HasBackendTrafficPolicyAnnotations() bool {
	return a.HasTimeout() || a.HasLoadBalancer() || a.has(ProxyBodySize)
}

// HasClientTrafficPolicyAnnotations returns true if any ClientTrafficPolicy annotation is present.
func (a AnnotationSet) HasClientTrafficPolicyAnnotations() bool {
	return a.has(ProxyBufferSize)
}

// HasSecurityPolicyAnnotations returns true if any SecurityPolicy annotation is present.
func (a AnnotationSet) HasSecurityPolicyAnnotations() bool {
	return a.HasCORS() || a.HasExtAuth()
}

// HasHTTPRouteFilters returns true if any HTTPRoute filter annotation is present.
func (a AnnotationSet) HasHTTPRouteFilters() bool {
	return a.HasRewrite() || a.HasAppRoot() || a.HasSSLRedirect()
}

func (a AnnotationSet) has(key string) bool {
	_, ok := a[key]
	return ok
}
