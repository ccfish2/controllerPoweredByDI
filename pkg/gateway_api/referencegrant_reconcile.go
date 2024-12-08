package gateway_api

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *referencegrantReconciler) Reconcile(context.Context, reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
