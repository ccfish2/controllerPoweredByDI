package gateway_api

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (g *grpcrouteReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// update grpc status
	return reconcile.Result{}, nil
}
