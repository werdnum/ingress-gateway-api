package config

import (
	"flag"
	"os"
)

// Config holds the controller configuration.
type Config struct {
	// GatewayName is the name of the shared Gateway resource from Envoy Gateway.
	GatewayName string

	// GatewayNamespace is the namespace of the shared Gateway resource.
	GatewayNamespace string

	// IngressClass filters which Ingresses to process. Empty means all.
	IngressClass string

	// MetricsAddr is the address the metrics endpoint binds to.
	MetricsAddr string

	// HealthProbeAddr is the address the health probe endpoint binds to.
	HealthProbeAddr string

	// LeaderElect enables leader election for controller manager.
	LeaderElect bool
}

// NewConfig creates a new Config with values from command line flags.
func NewConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.GatewayName, "gateway-name", getEnvOrDefault("GATEWAY_NAME", "eg-gateway"),
		"Name of the shared Gateway resource from Envoy Gateway")
	flag.StringVar(&cfg.GatewayNamespace, "gateway-namespace", getEnvOrDefault("GATEWAY_NAMESPACE", "envoy-gateway"),
		"Namespace of the shared Gateway resource")
	flag.StringVar(&cfg.IngressClass, "ingress-class", getEnvOrDefault("INGRESS_CLASS", ""),
		"Filter Ingresses by class (empty = process all)")
	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", ":8080",
		"The address the metrics endpoint binds to")
	flag.StringVar(&cfg.HealthProbeAddr, "health-probe-addr", ":8081",
		"The address the health probe endpoint binds to")
	flag.BoolVar(&cfg.LeaderElect, "leader-elect", false,
		"Enable leader election for controller manager")

	return cfg
}

// Parse parses the command line flags.
func (c *Config) Parse() {
	flag.Parse()
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
