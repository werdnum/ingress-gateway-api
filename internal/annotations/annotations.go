package annotations

// Nginx ingress annotation keys.
const (
	// Prefix is the common prefix for nginx ingress annotations.
	Prefix = "nginx.ingress.kubernetes.io/"

	// Timeout annotations
	ProxyReadTimeout = Prefix + "proxy-read-timeout"
	ProxySendTimeout = Prefix + "proxy-send-timeout"

	// Buffer annotations
	ProxyBufferSize = Prefix + "proxy-buffer-size"
	ProxyBodySize   = Prefix + "proxy-body-size"

	// Load balancer annotations
	UpstreamHashBy = Prefix + "upstream-hash-by"

	// CORS annotations
	CORSEnabled          = Prefix + "enable-cors"
	CORSAllowOrigin      = Prefix + "cors-allow-origin"
	CORSAllowMethods     = Prefix + "cors-allow-methods"
	CORSAllowHeaders     = Prefix + "cors-allow-headers"
	CORSExposeHeaders    = Prefix + "cors-expose-headers"
	CORSMaxAge           = Prefix + "cors-max-age"
	CORSAllowCredentials = Prefix + "cors-allow-credentials"

	// External auth annotations
	AuthURL             = Prefix + "auth-url"
	AuthSignin          = Prefix + "auth-signin"
	AuthResponseHeaders = Prefix + "auth-response-headers"

	// Rewrite annotations
	RewriteTarget = Prefix + "rewrite-target"
	AppRoot       = Prefix + "app-root"

	// SSL annotations
	SSLRedirect = Prefix + "ssl-redirect"

	// Path handling annotations
	UseRegex = Prefix + "use-regex"
)
