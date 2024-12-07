package gateway_api

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

func (t *tlsrouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// retrieve tls routes using namespaced name structure
	// update attach the rules to the gatway for routing
	return ctrl.Result{}, nil
}
