package gateway_api

import (
	"context"
	"log/slog"
	"os"

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
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	// myself
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
)

type httpRouteReconciler struct {
	Scheme *runtime.Scheme
	client.Client
}

func newhttpRouteReconciler(mgr ctrl.Manager) *httpRouteReconciler {
	return &httpRouteReconciler{
		mgr.GetScheme(),
		mgr.GetClient(),
	}
}

func (r *httpRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.HTTPRoute{}, backendServiceIndex,
		func(rawObj client.Object) []string {
			hr, ok := rawObj.(*gatewayv1.HTTPRoute)
			if !ok {
				return nil
			}
			var backendServices []string
			for _, rule := range hr.Spec.Rules {
				for _, backend := range rule.BackendRefs {
					if !helpers.IsService(backend.BackendObjectReference) {
						continue
					}
					backendServices = append(backendServices,
						types.NamespacedName{
							Namespace: helpers.NamespaceDerefOr(backend.Namespace, hr.Namespace),
							Name:      string(backend.Name),
						}.String(),
					)
				}
			}
			return backendServices
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1.HTTPRoute{}, gatewayIndex,
		func(rawObj client.Object) []string {
			hr := rawObj.(*gatewayv1.HTTPRoute)
			var gateways []string
			for _, parent := range hr.Spec.ParentRefs {
				if !helpers.IsGateway(parent) {
					continue
				}
				gateways = append(gateways,
					types.NamespacedName{
						Namespace: helpers.NamespaceDerefOr(parent.Namespace, hr.Namespace),
						Name:      string(parent.Name),
					}.String(),
				)
			}
			return gateways
		},
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.HTTPRoute{}).
		Watches(&corev1.Service{}, r.enqueueRequestForBackendService()).
		Watches(&gatewayv1beta1.ReferenceGrant{}, r.enqueueRequestForReferenceGrant()).
		Watches(&gatewayv1.Gateway{}, r.enqueueRequestForGateway(),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(hasMatchingController(context.Background(), mgr.GetClient(), controllerName, slog.New(slog.NewTextHandler(os.Stdout, nil)))))).
		Complete(r)
}

func (r *httpRouteReconciler) enqueueRequestForBackendService() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(backendServiceIndex))
}

func (r *httpRouteReconciler) enqueueRequestForReferenceGrant() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueAll())
}

func (r *httpRouteReconciler) enqueueRequestForGateway() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(gatewayIndex))
}

func (r *httpRouteReconciler) enqueueFromIndex(index string) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: "httpRoute",
			logfields.Resource:   client.ObjectKeyFromObject(o),
		})
		hrList := &gatewayv1.HTTPRouteList{}

		if err := r.Client.List(ctx, hrList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(index, client.ObjectKeyFromObject(o).String()),
		}); err != nil {
			scopedLog.WithError(err).Error("Failed to get related HTTPRoutes")
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(hrList.Items))
		for _, item := range hrList.Items {
			route := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: route,
			})
			scopedLog.WithField("httpRoute", route).Info("Enqueued HTTPRoute for resource")
		}
		return requests
	}
}

func (r *httpRouteReconciler) enqueueAll() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: "httpRoute",
			logfields.Resource:   client.ObjectKeyFromObject(o),
		})
		hrList := &gatewayv1.HTTPRouteList{}

		if err := r.Client.List(ctx, hrList, &client.ListOptions{}); err != nil {
			scopedLog.WithError(err).Error("Failed to get HTTPRoutes")
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(hrList.Items))
		for _, item := range hrList.Items {
			route := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: route,
			})
			scopedLog.WithField("httpRoute", route).Info("Enqueued HTTPRoute for resource")
		}
		return requests
	}
}
