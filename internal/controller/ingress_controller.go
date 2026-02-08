package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
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

// permanentError wraps an error to indicate it should not be retried immediately.
// The controller will requeue with a longer delay for permanent errors.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string {
	return e.err.Error()
}

func (e *permanentError) Unwrap() error {
	return e.err
}

// newPermanentError wraps an error to mark it as permanent.
func newPermanentError(err error) error {
	return &permanentError{err: err}
}

// isPermanentError checks if an error is marked as permanent.
func isPermanentError(err error) bool {
	var pe *permanentError
	return errors.As(err, &pe)
}

// permanentRequeueDelay is the delay before retrying permanent failures.
const permanentRequeueDelay = 5 * time.Minute

// handleReconcileError returns the appropriate Result based on error type.
// Permanent errors are requeued with a longer delay.
func handleReconcileError(err error) (ctrl.Result, error) {
	if isPermanentError(err) {
		// For permanent errors, requeue with a longer delay
		return ctrl.Result{RequeueAfter: permanentRequeueDelay}, nil
	}
	// For transient errors, let controller-runtime handle backoff
	return ctrl.Result{}, err
}

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
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=backendtrafficpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=clienttrafficpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=securitypolicies,verbs=get;list;watch;create;update;patch;delete
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
		patch := client.MergeFrom(ingress.DeepCopy())
		controllerutil.AddFinalizer(&ingress, FinalizerName)
		if err := r.Patch(ctx, &ingress, patch); err != nil {
			if apierrors.IsConflict(err) {
				logger.V(1).Info("Conflict adding Ingress finalizer, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Convert Ingress to HTTPRoutes and policies
	result := r.Converter.ConvertIngressFull(ctx, &ingress)

	// Create or update HTTPRoutes
	for _, httpRoute := range result.HTTPRoutes {
		if err := r.reconcileHTTPRoute(ctx, &ingress, httpRoute); err != nil {
			return handleReconcileError(err)
		}
	}

	// Create ReferenceGrant if needed for cross-namespace backend references
	if err := r.reconcileReferenceGrants(ctx, &ingress, result.HTTPRoutes); err != nil {
		return handleReconcileError(err)
	}

	// Reconcile BackendTrafficPolicies
	for _, btp := range result.BackendTrafficPolicies {
		if err := r.reconcileBackendTrafficPolicy(ctx, &ingress, btp); err != nil {
			return handleReconcileError(err)
		}
	}

	// Reconcile ClientTrafficPolicy
	if result.ClientTrafficPolicy != nil {
		if err := r.reconcileClientTrafficPolicy(ctx, &ingress, result.ClientTrafficPolicy); err != nil {
			return handleReconcileError(err)
		}
	}

	// Reconcile SecurityPolicies
	for _, sp := range result.SecurityPolicies {
		if err := r.reconcileSecurityPolicy(ctx, &ingress, sp); err != nil {
			return handleReconcileError(err)
		}
	}

	// Update Ingress status with Gateway address
	if err := r.updateIngressStatus(ctx, &ingress); err != nil {
		logger.Error(err, "failed to update Ingress status")
		// Don't return error - status update failure shouldn't block reconciliation
	}

	logger.Info("Successfully reconciled Ingress",
		"httpRoutes", len(result.HTTPRoutes),
		"backendTrafficPolicies", len(result.BackendTrafficPolicies),
		"securityPolicies", len(result.SecurityPolicies),
		"hasClientTrafficPolicy", result.ClientTrafficPolicy != nil)
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

		// Delete owned policies
		if err := r.deleteOwnedPolicies(ctx, ingress); err != nil {
			return ctrl.Result{}, err
		}

		// Remove finalizer using patch to avoid triggering admission webhooks
		patch := client.MergeFrom(ingress.DeepCopy())
		controllerutil.RemoveFinalizer(ingress, FinalizerName)
		if err := r.Patch(ctx, ingress, patch); err != nil {
			if apierrors.IsConflict(err) {
				logger.V(1).Info("Conflict removing finalizer, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
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

// deleteOwnedPolicies deletes all policy resources owned by the Ingress.
func (r *IngressReconciler) deleteOwnedPolicies(ctx context.Context, ingress *networkingv1.Ingress) error {
	logger := log.FromContext(ctx)
	sourceRef := fmt.Sprintf("%s/%s", ingress.Namespace, ingress.Name)

	// Delete BackendTrafficPolicies
	var btpList egv1alpha1.BackendTrafficPolicyList
	if err := r.List(ctx, &btpList, client.InNamespace(ingress.Namespace)); err != nil {
		return err
	}
	for _, btp := range btpList.Items {
		if btp.Annotations[SourceAnnotation] == sourceRef {
			if err := r.Delete(ctx, &btp); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			logger.Info("Deleted BackendTrafficPolicy", "name", btp.Name)
		}
	}

	// Delete ClientTrafficPolicies
	var ctpList egv1alpha1.ClientTrafficPolicyList
	if err := r.List(ctx, &ctpList, client.InNamespace(ingress.Namespace)); err != nil {
		return err
	}
	for _, ctp := range ctpList.Items {
		if ctp.Annotations[SourceAnnotation] == sourceRef {
			if err := r.Delete(ctx, &ctp); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			logger.Info("Deleted ClientTrafficPolicy", "name", ctp.Name)
		}
	}

	// Delete SecurityPolicies
	var spList egv1alpha1.SecurityPolicyList
	if err := r.List(ctx, &spList, client.InNamespace(ingress.Namespace)); err != nil {
		return err
	}
	for _, sp := range spList.Items {
		if sp.Annotations[SourceAnnotation] == sourceRef {
			if err := r.Delete(ctx, &sp); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			logger.Info("Deleted SecurityPolicy", "name", sp.Name)
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
				if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
					logger.Error(err, "Invalid HTTPRoute, will retry with longer delay", "name", httpRoute.Name)
					return newPermanentError(err)
				}
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
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			logger.Error(err, "Invalid HTTPRoute update, will retry with longer delay", "name", httpRoute.Name)
			return newPermanentError(err)
		}
		return err
	}
	logger.Info("Updated HTTPRoute", "name", httpRoute.Name)
	return nil
}

// reconcileBackendTrafficPolicy creates or updates a BackendTrafficPolicy.
func (r *IngressReconciler) reconcileBackendTrafficPolicy(ctx context.Context, ingress *networkingv1.Ingress, policy *egv1alpha1.BackendTrafficPolicy) error {
	logger := log.FromContext(ctx)

	// Set namespace to match Ingress
	policy.Namespace = ingress.Namespace

	// Set owner reference
	converter.SetPolicyOwnerReference(policy, ingress)

	// Check if policy exists
	existing := &egv1alpha1.BackendTrafficPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(policy), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, policy); err != nil {
				if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
					logger.Error(err, "Invalid BackendTrafficPolicy, will retry with longer delay", "name", policy.Name)
					return newPermanentError(err)
				}
				return err
			}
			logger.Info("Created BackendTrafficPolicy", "name", policy.Name)
			return nil
		}
		return err
	}

	// Update existing policy
	existing.Spec = policy.Spec
	existing.Annotations = policy.Annotations
	existing.Labels = policy.Labels
	existing.OwnerReferences = policy.OwnerReferences

	if err := r.Update(ctx, existing); err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			logger.Error(err, "Invalid BackendTrafficPolicy update, will retry with longer delay", "name", policy.Name)
			return newPermanentError(err)
		}
		return err
	}
	logger.Info("Updated BackendTrafficPolicy", "name", policy.Name)
	return nil
}

// reconcileClientTrafficPolicy creates or updates a ClientTrafficPolicy.
func (r *IngressReconciler) reconcileClientTrafficPolicy(ctx context.Context, ingress *networkingv1.Ingress, policy *egv1alpha1.ClientTrafficPolicy) error {
	logger := log.FromContext(ctx)

	// Set namespace to match Ingress
	policy.Namespace = ingress.Namespace

	// Set owner reference
	converter.SetPolicyOwnerReference(policy, ingress)

	// Check if policy exists
	existing := &egv1alpha1.ClientTrafficPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(policy), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, policy); err != nil {
				if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
					logger.Error(err, "Invalid ClientTrafficPolicy, will retry with longer delay", "name", policy.Name)
					return newPermanentError(err)
				}
				return err
			}
			logger.Info("Created ClientTrafficPolicy", "name", policy.Name)
			return nil
		}
		return err
	}

	// Update existing policy
	existing.Spec = policy.Spec
	existing.Annotations = policy.Annotations
	existing.Labels = policy.Labels
	existing.OwnerReferences = policy.OwnerReferences

	if err := r.Update(ctx, existing); err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			logger.Error(err, "Invalid ClientTrafficPolicy update, will retry with longer delay", "name", policy.Name)
			return newPermanentError(err)
		}
		return err
	}
	logger.Info("Updated ClientTrafficPolicy", "name", policy.Name)
	return nil
}

// reconcileSecurityPolicy creates or updates a SecurityPolicy.
func (r *IngressReconciler) reconcileSecurityPolicy(ctx context.Context, ingress *networkingv1.Ingress, policy *egv1alpha1.SecurityPolicy) error {
	logger := log.FromContext(ctx)

	// Set namespace to match Ingress
	policy.Namespace = ingress.Namespace

	// Set owner reference
	converter.SetPolicyOwnerReference(policy, ingress)

	// Check if policy exists
	existing := &egv1alpha1.SecurityPolicy{}
	err := r.Get(ctx, client.ObjectKeyFromObject(policy), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, policy); err != nil {
				if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
					logger.Error(err, "Invalid SecurityPolicy, will retry with longer delay", "name", policy.Name)
					return newPermanentError(err)
				}
				return err
			}
			logger.Info("Created SecurityPolicy", "name", policy.Name)
			return nil
		}
		return err
	}

	// Update existing policy
	existing.Spec = policy.Spec
	existing.Annotations = policy.Annotations
	existing.Labels = policy.Labels
	existing.OwnerReferences = policy.OwnerReferences

	if err := r.Update(ctx, existing); err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			logger.Error(err, "Invalid SecurityPolicy update, will retry with longer delay", "name", policy.Name)
			return newPermanentError(err)
		}
		return err
	}
	logger.Info("Updated SecurityPolicy", "name", policy.Name)
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

// updateIngressStatus updates the Ingress status with the Gateway's load balancer address.
// Uses the status subresource to avoid conflicts with spec updates.
func (r *IngressReconciler) updateIngressStatus(ctx context.Context, ingress *networkingv1.Ingress) error {
	logger := log.FromContext(ctx)

	// Look up the Gateway to get its addresses
	gateway := &gatewayv1.Gateway{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: r.Config.GatewayNamespace,
		Name:      r.Config.GatewayName,
	}, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Gateway not found, skipping status update")
			return nil
		}
		return err
	}

	// Build the load balancer ingress list from Gateway addresses
	var lbIngress []networkingv1.IngressLoadBalancerIngress
	for _, addr := range gateway.Status.Addresses {
		ing := networkingv1.IngressLoadBalancerIngress{}
		if addr.Type != nil && *addr.Type == gatewayv1.HostnameAddressType {
			ing.Hostname = addr.Value
		} else {
			ing.IP = addr.Value
		}
		lbIngress = append(lbIngress, ing)
	}

	// Check if status needs updating
	if ingressStatusEqual(ingress.Status.LoadBalancer.Ingress, lbIngress) {
		return nil
	}

	// Update Ingress status using status subresource
	ingress.Status.LoadBalancer.Ingress = lbIngress
	if err := r.Status().Update(ctx, ingress); err != nil {
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Conflict updating Ingress status, will be retried")
		}
		return err
	}

	logger.Info("Updated Ingress status", "addresses", len(lbIngress))
	return nil
}

// ingressStatusEqual checks if two IngressLoadBalancerIngress slices are equal.
func ingressStatusEqual(a, b []networkingv1.IngressLoadBalancerIngress) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].IP != b[i].IP || a[i].Hostname != b[i].Hostname {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Owns(&egv1alpha1.BackendTrafficPolicy{}).
		Owns(&egv1alpha1.ClientTrafficPolicy{}).
		Owns(&egv1alpha1.SecurityPolicy{}).
		Complete(r)
}
