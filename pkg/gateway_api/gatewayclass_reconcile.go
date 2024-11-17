package gatewayapi

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
