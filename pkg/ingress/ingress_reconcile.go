package ingress

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *ingressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// scopedlog the controller is ingress
	// read ingress objects from kubeapi server client with cached which namespace and name is the same as from req

	// check metaobject creationtimestamp fields, if it is set, this is foreground deletion, skip this
	// until finalizers are empty

	// if the ingress is not managed by dolphin anymore, remove and clean them
	// update routing within the load balancer
	return ctrl.Result{}, nil
}
