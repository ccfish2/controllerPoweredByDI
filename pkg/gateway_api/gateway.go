package gateway_api

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

// return an event handler for all Gateway objects belonging to the given GatewayClass
func (r *gatewayReconciler) enqueueRequestForOwningGatewayClass() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		var reqs []reconcile.Request
		gwList := &gatewayv1.GatewayList{}
		err := r.Client.List(ctx, gwList)
		if err != nil {
			fmt.Println("Unable to list gateways")
			return nil
		}

		for _, gw := range gwList.Items {
			if gw.Spec.GatewayClassName != gatewayv1.ObjectName(obj.GetName()) {
				continue
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gw.Namespace,
					Name:      gw.Name,
				},
			}
			reqs = append(reqs, req)
			fmt.Println("Queueing gateway")
		}

		return reqs
	})
}
