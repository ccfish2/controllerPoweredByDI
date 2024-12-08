package gateway_api

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type grpcrouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func newgrpcrouteReconciler(mgr ctrl.Manager) *grpcrouteReconciler {
	return &grpcrouteReconciler{
		mgr.GetClient(),
		mgr.GetScheme(),
	}
}

func (g *grpcrouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gwv1.GRPCRoute{}, backendServiceIndex, func(o client.Object) []string {
		// add index fields for the grpc route of field name
		return []string{}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gwv1alpha2.GRPCRoute{}, gatewayIndex, func(o client.Object) []string {
		return []string{}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1.GRPCRoute{}).
		Watches(&gwv1.GRPCRoute{}, g.enqeuueRequestgrpc()).
		Complete(g)
}

func (g *grpcrouteReconciler) enqeuueRequestgrpc() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{}
	})
}
