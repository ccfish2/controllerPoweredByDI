package watchhandlers

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/helpers"
	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/indexers"
	"github.com/ccfish2/infra/pkg/logging/logfields"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// returns an event handler that, when passed a BackendTLSPolicy
// returns reconcile.Requests
// for all controller-relevant Gateway where that BackendTLS policy references a service
// that is used as a backend for a route
// that is attached to that gateway
func EnqueueRequestForBackendTLSPolicy(c client.Client, logger *slog.Logger,
	controllerName string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, o client.Object) []reconcile.Request {
			scopedLog := logger.With("queue-gw-from-backendtlspolicy")

			reconcileRequests := make(map[reconcile.Request]struct{})
			btlsp, ok := o.(*gatewayv1.BackendTLSPolicy)
			if !ok {
				return nil
			}

			allGatewaysSet, err := getAllGatewaysSetForController(ctx, c, controllerName)
			if err != nil {
				scopedLog.ErrorContext(ctx, "Failed to get controller Gateways", logfields.Error, err)
				return []reconcile.Request{}
			}

			ns := o.GetNamespace()
			updateReconcileRequestsForBackendTLSPolicy(ctx, c, scopedLog, allGatewaysSet, reconcileRequests, btlsp, ns)

			recs := slices.Collect(maps.Keys(reconcileRequests))
			if len(recs) > 0 {
				scopedLog.Debug("BackendTLSPolicy relevant to Gateways",
					logfields.Resource, client.ObjectKeyFromObject(o).String(),
					logfields.Gateway, recs)
			}
			return slices.Collect(maps.Keys(reconcileRequests))
		})
}

func updateReconcileRequestsForBackendTLSPolicy(ctx context.Context,
	c client.Client,
	scopedlog *slog.Logger,
	allGatewaySet map[string]struct{},
	rrSet map[reconcile.Request]struct{},
	btlsp *gatewayv1.BackendTLSPolicy,
	ns string) {
	serviceRefs := []string{}

	for _, target := range btlsp.Spec.TargetRefs {
		if helpers.IsServiceTargetRef(target) {
			serviceRefs = append(serviceRefs, ns+"/"+string(target.Name))
		}
	}
	httpRoutes := []gatewayv1.HTTPRoute{}

	for _, svcName := range serviceRefs {
		hrList := &gatewayv1.HTTPRouteList{}

		if err := c.List(ctx, hrList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				indexers.BackendServiceHTTPRouteIndex, svcName,
			),
		}); err != nil {
			scopedlog.ErrorContext(ctx, "Failed to get HTTPRoutes",
				logfields.Error, err)
			return
		}
	}
	for _, hr := range httpRoutes {
		updateReconcileRequestsForParentRefs(ctx, c, hr.Spec.ParentRefs, hr.Namespace, allGatewaySet, rrSet)
	}
}
