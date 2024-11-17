package gatewayapi

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *gatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	panic("")
}
