package gateway_api

import (
	"context"

	"github.com/ccfish2/infra/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func (r *gatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scopedLog := log.WithContext(ctx).WithFields(logrus.Fields{
		logfields.Controller: "gatewayclass",
		logfields.Resource:   req.NamespacedName,
	})
	scopedLog.Info("Reconciling gateway class")
	gwc := gatewayv1.GatewayClass{}
	if err := r.Client.Get(ctx, req.NamespacedName, &gwc); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}
	if gwc.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}
	setGatewayClassAccepted(&gwc, true)

	gwc.Status.SupportedFeatures = []gatewayv1.SupportedFeature{
		{"HTTPRoute"},
		{"HTTPRouteDestinationPortMatching"},
		{"TLSRoute"},
	}
	if err := r.Client.Update(ctx, &gwc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
