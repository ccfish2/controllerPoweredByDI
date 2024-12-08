package gateway_api

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type referencegrantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func newreferencegrantReconciler(mgr ctrl.Manager) *referencegrantReconciler {
	return &referencegrantReconciler{
		mgr.GetClient(),
		mgr.GetScheme(),
	}
}

func (g *referencegrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gwv1beta1.ReferenceGrant{}, backendServiceIndex, func(o client.Object) []string {
		// add index fields for the grpc route of field name
		return []string{}
	}); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gwv1beta1.ReferenceGrant{}, gatewayIndex, func(o client.Object) []string {
		return []string{}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.ReferenceGrant{}).
		Watches(&gwv1beta1.ReferenceGrant{}, g.enqeuueRequest()).
		Complete(g)
}

func (g *referencegrantReconciler) enqeuueRequest() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		return []reconcile.Request{}
	})
}
