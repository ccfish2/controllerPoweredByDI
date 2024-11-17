package gatewayapi

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type gatewayReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	SecretNamespace    string
	IdleTimeoutSeconds int
}

func newGatewayReconciler(mgr ctrl.Manager, secretsNamespace string, idleTimeoutSeconds int) *gatewayReconciler {
	return &gatewayReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		SecretNamespace:    secretsNamespace,
		IdleTimeoutSeconds: idleTimeoutSeconds,
	}
}

// sets up the controller with the Manager
// The reconciler will be triggere by Gateway, or any dolphin-managed GatewayClass events
// Endpoints
func (r *gatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	hasMatchingControllerFn := hasMatchingController(context.Background(), r.Client, controllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{},
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingControllerFn))).
		Watches(&gatewayv1.GatewayClass{},
			r.enqueueRequestForOwningGatewayClass(),
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingControllerFn))).
		Owns(&corev1.Service{}).
		Owns(&corev1.Endpoints{}).
		Complete(r)
}

func (r *gatewayReconciler) enqueueRequestForOwningGatewayClass() handler.EventHandler {
	panic("")
}
