package controller

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/werdnum/ingress-gateway-api/internal/config"
	"github.com/werdnum/ingress-gateway-api/internal/converter"
)

const (
	// FinalizerName is the finalizer used by this controller.
	FinalizerName = "ingress-gateway-api.io/finalizer"

	// SourceAnnotation tracks the source Ingress for an HTTPRoute.
	SourceAnnotation = "ingress-gateway-api.io/source"
)

// IngressReconciler reconciles Ingress resources.
type IngressReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Config    *config.Config
	Converter *converter.Converter
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Ingress reconciliation.
func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Ingress
	var ingress networkingv1.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ingress); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Ingress not found, skipping")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if Ingress class matches filter
	if !r.shouldProcess(&ingress) {
		logger.V(1).Info("Ingress class does not match filter, skipping",
			"ingressClass", r.getIngressClass(&ingress),
			"filter", r.Config.IngressClass)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !ingress.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &ingress)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&ingress, FinalizerName) {
		controllerutil.AddFinalizer(&ingress, FinalizerName)
		if err := r.Update(ctx, &ingress); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Convert Ingress to HTTPRoutes
	httpRoutes := r.Converter.ConvertIngress(&ingress)

	// Create or update HTTPRoutes
	for _, httpRoute := range httpRoutes {
		if err := r.reconcileHTTPRoute(ctx, &ingress, httpRoute); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create ReferenceGrant if needed for cross-namespace backend references
	if err := r.reconcileReferenceGrants(ctx, &ingress, httpRoutes); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled Ingress", "httpRoutes", len(httpRoutes))
	return ctrl.Result{}, nil
}

// shouldProcess checks if the Ingress should be processed based on the ingress class filter.
func (r *IngressReconciler) shouldProcess(ingress *networkingv1.Ingress) bool {
	if r.Config.IngressClass == "" {
		return true
	}
	return r.getIngressClass(ingress) == r.Config.IngressClass
}

// getIngressClass returns the ingress class of the Ingress.
func (r *IngressReconciler) getIngressClass(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	// Check deprecated annotation
	if class, ok := ingress.Annotations["kubernetes.io/ingress.class"]; ok {
		return class
	}
	return ""
}

// handleDeletion handles Ingress deletion by cleaning up owned resources.
func (r *IngressReconciler) handleDeletion(ctx context.Context, ingress *networkingv1.Ingress) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(ingress, FinalizerName) {
		// Delete owned HTTPRoutes
		if err := r.deleteOwnedHTTPRoutes(ctx, ingress); err != nil {
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(ingress, FinalizerName)
		if err := r.Update(ctx, ingress); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Finalizer removed, cleanup complete")
	}

	return ctrl.Result{}, nil
}

// deleteOwnedHTTPRoutes deletes HTTPRoutes owned by the Ingress.
func (r *IngressReconciler) deleteOwnedHTTPRoutes(ctx context.Context, ingress *networkingv1.Ingress) error {
	logger := log.FromContext(ctx)

	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes, client.InNamespace(ingress.Namespace)); err != nil {
		return err
	}

	sourceRef := fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name)
	for _, route := range httpRoutes.Items {
		if route.Annotations[SourceAnnotation] == sourceRef {
			if err := r.Delete(ctx, &route); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			logger.Info("Deleted HTTPRoute", "name", route.Name)
		}
	}

	return nil
}

// reconcileHTTPRoute creates or updates an HTTPRoute.
func (r *IngressReconciler) reconcileHTTPRoute(ctx context.Context, ingress *networkingv1.Ingress, httpRoute *gatewayv1.HTTPRoute) error {
	logger := log.FromContext(ctx)

	// Set namespace to match Ingress
	httpRoute.Namespace = ingress.Namespace

	// Set owner reference
	converter.SetOwnerReference(httpRoute, ingress)

	// Check if HTTPRoute exists
	existing := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKeyFromObject(httpRoute), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new HTTPRoute
			if err := r.Create(ctx, httpRoute); err != nil {
				return err
			}
			logger.Info("Created HTTPRoute", "name", httpRoute.Name)
			return nil
		}
		return err
	}

	// Update existing HTTPRoute
	existing.Spec = httpRoute.Spec
	existing.Annotations = httpRoute.Annotations
	existing.Labels = httpRoute.Labels
	existing.OwnerReferences = httpRoute.OwnerReferences

	if err := r.Update(ctx, existing); err != nil {
		return err
	}
	logger.Info("Updated HTTPRoute", "name", httpRoute.Name)
	return nil
}

// reconcileReferenceGrants creates ReferenceGrants for cross-namespace backend references.
func (r *IngressReconciler) reconcileReferenceGrants(ctx context.Context, ingress *networkingv1.Ingress, httpRoutes []*gatewayv1.HTTPRoute) error {
	logger := log.FromContext(ctx)

	// Collect unique backend namespaces that differ from the HTTPRoute namespace
	backendNamespaces := make(map[string]struct{})
	for _, route := range httpRoutes {
		for _, rule := range route.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				if backendRef.Namespace != nil && string(*backendRef.Namespace) != route.Namespace {
					backendNamespaces[string(*backendRef.Namespace)] = struct{}{}
				}
			}
		}
	}

	// Create ReferenceGrant in each backend namespace
	for ns := range backendNamespaces {
		grant := &gatewayv1beta1.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("ingress-%s-%s", ingress.Namespace, ingress.Name),
				Namespace: ns,
				Annotations: map[string]string{
					SourceAnnotation: fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name),
				},
			},
			Spec: gatewayv1beta1.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{
					{
						Group:     gatewayv1.Group("gateway.networking.k8s.io"),
						Kind:      "HTTPRoute",
						Namespace: gatewayv1.Namespace(ingress.Namespace),
					},
				},
				To: []gatewayv1beta1.ReferenceGrantTo{
					{
						Group: "",
						Kind:  "Service",
					},
				},
			},
		}

		existing := &gatewayv1beta1.ReferenceGrant{}
		err := r.Get(ctx, client.ObjectKeyFromObject(grant), existing)
		if err != nil {
			if apierrors.IsNotFound(err) {
				if err := r.Create(ctx, grant); err != nil {
					return err
				}
				logger.Info("Created ReferenceGrant", "namespace", ns, "name", grant.Name)
				continue
			}
			return err
		}

		// Update if needed
		existing.Spec = grant.Spec
		if err := r.Update(ctx, existing); err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Complete(r)
}
