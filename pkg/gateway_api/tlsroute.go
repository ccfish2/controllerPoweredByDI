package gateway_api

import (
	"context"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
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
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1alpha2.TLSRoute{}, backendServiceIndex,
		func(rawObj client.Object) []string {
			hr, ok := rawObj.(*gatewayv1alpha2.TLSRoute)
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

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1alpha2.TLSRoute{}, backendServiceImportIndex,
		func(rawObj client.Object) []string {
			hr, ok := rawObj.(*gatewayv1alpha2.TLSRoute)
			if !ok {
				return nil
			}
			var backendServiceImports []string
			for _, rule := range hr.Spec.Rules {
				for _, backend := range rule.BackendRefs {
					if !helpers.IsServiceImport(backend.BackendObjectReference) {
						continue
					}
					backendServiceImports = append(backendServiceImports,
						types.NamespacedName{
							Namespace: helpers.NamespaceDerefOr(backend.Namespace, hr.Namespace),
							Name:      string(backend.Name),
						}.String(),
					)
				}
			}
			return backendServiceImports
		},
	); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayv1alpha2.TLSRoute{}, gatewayIndex,
		func(rawObj client.Object) []string {
			hr := rawObj.(*gatewayv1alpha2.TLSRoute)
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
		For(&gatewayv1alpha2.TLSRoute{}).
		Watches(&corev1.Service{}, t.enqueueRequestForBackendService()).
		Watches(&gatewayv1beta1.ReferenceGrant{}, t.enqueueRequestForReferenceGrant()).
		Watches(&gatewayv1.Gateway{}, t.enqueueRequestForGateway(),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(hasMatchingController(context.Background(), mgr.GetClient(), controllerName)),
			)).
		Complete(t)
}

func (r *tlsrouteReconciler) enqueueRequestForGateway() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(gatewayIndex))
}

func (r *tlsrouteReconciler) enqueueAll() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: "tlsRoute",
			logfields.Resource:   client.ObjectKeyFromObject(o),
		})
		trList := &gatewayv1alpha2.TLSRouteList{}

		if err := r.Client.List(ctx, trList, &client.ListOptions{}); err != nil {
			scopedLog.WithError(err).Error("Failed to get TLSRoutes")
			return []reconcile.Request{}
		}

		requests := make([]reconcile.Request, 0, len(trList.Items))
		for _, item := range trList.Items {
			route := client.ObjectKey{
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}
			requests = append(requests, reconcile.Request{
				NamespacedName: route,
			})
			scopedLog.WithField("tlsRoute", route).Info("Enqueued TLSRoute for resource")
		}
		return requests
	}
}

func (r *tlsrouteReconciler) enqueueRequestForReferenceGrant() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueAll())
}

func (r *tlsrouteReconciler) equeGateway() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.equeGatewayFromIndex(gatewayIndex))
}

func (r *tlsrouteReconciler) enqueueRequestForBackendService() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(r.enqueueFromIndex(backendServiceIndex))
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

func (r *tlsrouteReconciler) enqueueFromIndex(index string) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		rList := &gatewayv1alpha2.TLSRouteList{}
		if err := r.Client.List(context.Background(), rList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(index, client.ObjectKeyFromObject(o).String()),
		}); err != nil {
			return []reconcile.Request{}
		}
		res := []reconcile.Request{}
		for _, tl := range rList.Items {
			route := client.ObjectKey{
				Namespace: tl.Namespace,
				Name:      tl.Name,
			}
			res = append(res, reconcile.Request{
				NamespacedName: route,
			})
		}
		return res
	}
}
