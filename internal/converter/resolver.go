package converter

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServicePortResolver resolves named service ports to numeric ports.
type ServicePortResolver interface {
	// ResolvePort resolves a service port (by name or number) to a numeric port.
	// If portName is empty and portNumber is 0, returns an error.
	// If portNumber is non-zero, returns it directly.
	// If portName is set, looks up the service to find the numeric port.
	ResolvePort(ctx context.Context, namespace, serviceName, portName string, portNumber int32) (int32, error)
}

// ClientServicePortResolver implements ServicePortResolver using a Kubernetes client.
type ClientServicePortResolver struct {
	client client.Client
}

// NewServicePortResolver creates a new ServicePortResolver.
func NewServicePortResolver(c client.Client) ServicePortResolver {
	return &ClientServicePortResolver{client: c}
}

// ResolvePort resolves a service port to a numeric port.
func (r *ClientServicePortResolver) ResolvePort(ctx context.Context, namespace, serviceName, portName string, portNumber int32) (int32, error) {
	// If numeric port is specified, use it directly
	if portNumber != 0 {
		return portNumber, nil
	}

	// If no port name, we can't resolve
	if portName == "" {
		return 0, fmt.Errorf("no port specified for service %s/%s", namespace, serviceName)
	}

	// Look up the service to resolve the named port
	svc := &corev1.Service{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceName}, svc); err != nil {
		return 0, fmt.Errorf("failed to get service %s/%s: %w", namespace, serviceName, err)
	}

	// Find the port by name
	for _, port := range svc.Spec.Ports {
		if port.Name == portName {
			return port.Port, nil
		}
	}

	return 0, fmt.Errorf("port %q not found in service %s/%s", portName, namespace, serviceName)
}

// NoopServicePortResolver is a resolver that doesn't resolve named ports.
// Used for testing or when no client is available.
type NoopServicePortResolver struct{}

// ResolvePort returns the numeric port if set, otherwise returns an error for named ports.
func (r *NoopServicePortResolver) ResolvePort(ctx context.Context, namespace, serviceName, portName string, portNumber int32) (int32, error) {
	if portNumber != 0 {
		return portNumber, nil
	}
	return 0, fmt.Errorf("named port %q cannot be resolved without service lookup", portName)
}
