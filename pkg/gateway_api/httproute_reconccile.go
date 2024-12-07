package gateway_api

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/ccfish2/controllerPoweredByDI/pkg/gateway_api/routechecker"
)

func (r *httpRouteReonciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	org := gatewayv1.HTTPRoute{}
	if err := r.Client.Get(ctx, req.NamespacedName, &org); err != nil {
		return ctrl.Result{}, err
	}

	hr := org.DeepCopy()

	grants := gatewayv1beta1.ReferenceGrantList{}
	if err := r.Client.List(ctx, &grants); err != nil {
		return ctrl.Result{}, err
	}
	_ = routechecker.HTTPRouteInput{}
	for _, parent := range hr.Spec.ParentRefs {
		// rune
		r.setParentCondition(parent)
		// run routecker functions
	}
	if err := r.Client.Update(ctx, hr); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *httpRouteReonciler) setParentCondition(parent gatewayv1.ParentReference) {

}
