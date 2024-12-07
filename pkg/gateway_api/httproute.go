package gateway_api

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
)

const (
	backendServiceIndex string = "backendServiceIndex"
	gatewayIndex        string = "gatewayIndex"
)

type httpRouteReonciler struct {
	Scheme *runtime.Scheme
	client.Client
}

func newhttpRouteReonciler(mgr ctrl.Manager) *httpRouteReonciler {
	return &httpRouteReonciler{
		mgr.GetScheme(),
		mgr.GetClient(),
	}
}

func (r *httpRouteReonciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.HTTPRoute{}, backendServiceIndex, func(rawObj client.Object) []string {
		hr := rawObj.(*gatewayv1.HTTPRoute)
		backenServices := []string{}
		for _, rule := range hr.Spec.Rules {
			for _, parent := range rule.BackendRefs {
				if !helpers.IsService(parent.BackendObjectReference) {
					continue
				}
				backenServices = append(backenServices, types.NamespacedName{}.String())
			}
		}
		return backenServices
	}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.Gateway{}, gatewayIndex, func(o client.Object) []string {
		gws := []string{}
		// filter the gotten gw based on filter selector
		return gws
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Watches(&corev1.Service{}, r.enqueueRequestForGW(),
			builder.WithPredicates(predicate.NewPredicateFuncs(hasMatchingController(context.Background(), mgr.GetClient(), "io.dolphin/gateway-controller")))).
		Complete(r)
}

func (r *httpRouteReonciler) enqueueRequestForGW() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueRequestForIndex(gatewayIndex))
}

func (r *httpRouteReonciler) enqueueRequestForIndex(index string) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		hrList := gatewayv1.HTTPRouteList{}
		if err := r.Client.List(context.Background(), &hrList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(index, client.ObjectKeyFromObject(o).String()),
		}); err != nil {
			return nil
		}
		req := []reconcile.Request{}
		for _, hr := range hrList.Items {

			r := client.ObjectKey{Namespace: hr.Namespace, Name: hr.Name}
			req = append(req, reconcile.Request{NamespacedName: r})
		}
		return []reconcile.Request{}
	}
}

func (r *httpRouteReonciler) enqueueAll() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		req := []reconcile.Request{}
		gws := gatewayv1.GatewayList{}
		if err := r.Client.List(ctx, &gws, &client.ListOptions{}); err != nil {
			return nil
		}
		// iterate the gwlist and append them to the function return
		return req
	}
}
