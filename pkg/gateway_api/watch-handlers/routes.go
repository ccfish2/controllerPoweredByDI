package watchhandlers

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func EnqueueRequestForOwningTCPRoute(c client.Client, logger *slog.Logger, controllerName string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		tr, ok := a.(*gatewayv1.TCPRoute)
		if !ok {
			return nil
		}
		return getGatewayReconcileRequestsForRoute(context.Background(), c, a, tr.Spec.CommonRouteSpec, logger, controllerName)
	})
}
