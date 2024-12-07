package gateway_api

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type tlsrouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func newtlsrouteReconciler(mgr ctrl.Manager) *tlsrouteReconciler {
	return &tlsrouteReconciler{
		mgr.GetClient(),
		mgr.GetScheme(),
	}
}

func (t *tlsrouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1alpha2.TCPRoute{}, backendServiceIndex, func(o client.Object) []string {
		backendServices := []string{}
		// iterate the tlsroute.Spec.Rules.BackendRefs
		// check if it is a backendService
		// compose namespaced name objecct, and convert to string
		return backendServices
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.Gateway{}, gatewayIndex, func(o client.Object) []string {
		gws := []string{}
		// list all gateways belonging to dolphin GWC and belongs to io.dolphin/gateway-controller
		// compose namespacedname
		return gws
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1alpha2.TCPRoute{}).
		Watches(&gatewayv1.Gateway{}, t.equeGateway(),
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingController(context.Background(), mgr.GetClient(), controllerName)))).
		Complete(t)
}

func (r *tlsrouteReconciler) equeGateway() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.equeGatewayFromIndex(gatewayIndex))
}

func (r *tlsrouteReconciler) equeGatewayFromIndex(index string) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		tlsRouteList := gatewayv1alpha2.TLSRouteList{}
		if err := r.Client.List(context.Background(), &tlsRouteList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(index, client.ObjectKeyFromObject(o).String())}); err != nil {
			return nil
		}
		return []reconcile.Request{}
	}
}
